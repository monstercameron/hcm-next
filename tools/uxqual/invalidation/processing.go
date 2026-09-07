package invalidation

import (
	"context"
	"errors"
	"io"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func (c *Client) read(run *runState) {
	var terminal error
	defer func() {
		close(run.jobs)
		<-run.workerDone
		run.cancel()
		c.interrupt(run)
		c.mu.Lock()
		if c.run == run {
			c.run = nil
			c.pending = nil
		}
		c.mu.Unlock()
		c.observe(Event{Kind: EventClosed})
		run.done <- terminal
		close(run.done)
	}()
	for {
		if err := run.ctx.Err(); err != nil {
			terminal = err
			return
		}
		raw, err := safeRecv(run.stream)
		if err != nil {
			if contextErr := run.ctx.Err(); contextErr != nil {
				terminal = contextErr
			} else if !errors.Is(err, io.EOF) {
				terminal = ErrStreamReceive
			}
			return
		}
		c.handle(run, raw)
	}
}

func safeRecv(stream Stream) (raw []byte, err error) {
	defer func() {
		if recover() != nil {
			raw = nil
			err = ErrStreamReceive
		}
	}()
	return stream.Recv()
}

func (c *Client) worker(run *runState) {
	defer close(run.workerDone)
	for {
		select {
		case <-run.ctx.Done():
			return
		case job, ok := <-run.jobs:
			if !ok {
				return
			}
			<-job.ready
			err := c.callRefetch(run, job.refresh)
			c.complete(run, job, err)
			if run.ctx.Err() != nil {
				return
			}
		}
	}
}

func (c *Client) callRefetch(run *runState, refresh Refresh) error {
	result := make(chan error, 1)
	go func() {
		var err error
		defer func() {
			if recover() != nil {
				err = ErrRefetchFailed
			}
			result <- err
		}()
		err = c.refetch(run.ctx, refresh)
	}()
	select {
	case err := <-result:
		return err
	case <-run.ctx.Done():
		return run.ctx.Err()
	}
}

func (c *Client) complete(run *runState, job refreshJob, err error) {
	c.mu.Lock()
	for i := range c.pending {
		if c.pending[i].id == job.id {
			c.pending = append(c.pending[:i], c.pending[i+1:]...)
			break
		}
	}
	queued := len(run.jobs)
	current := c.run == run && run.ctx.Err() == nil
	if current && err == nil {
		if job.refresh.SourceSequence > c.scope.SourceSequence {
			c.scope.SourceSequence = job.refresh.SourceSequence
		}
		if job.refresh.Watermark > c.scope.Watermark {
			c.scope.Watermark = job.refresh.Watermark
		}
		for key, revision := range job.revisions {
			if revision > c.scope.SubjectRevisions[key] {
				c.scope.SubjectRevisions[key] = revision
			}
		}
		c.refetched++
	} else if current && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		c.refetchErrors++
	}
	c.mu.Unlock()
	if !current {
		return
	}
	if err == nil {
		c.observe(Event{Kind: EventRefetched, QueueLength: queued, SourceSequence: job.refresh.SourceSequence})
		return
	}
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		c.observe(Event{Kind: EventRefetchErr, QueueLength: queued, SourceSequence: job.refresh.SourceSequence})
	}
}

func (c *Client) handle(run *runState, raw []byte) {
	message, err := Decode(raw, c.options.MaxMessageBytes)
	if err != nil {
		c.reject(run, EventRejected)
		return
	}
	c.mu.Lock()
	if c.run != run {
		c.mu.Unlock()
		return
	}
	scope := c.scope
	admissionSequence := scope.SourceSequence
	admissionWatermark := scope.Watermark
	admissionRevisions := make(map[string]uint64, len(scope.SubjectRevisions)+len(c.pending))
	for key, revision := range scope.SubjectRevisions {
		admissionRevisions[key] = revision
	}
	for _, pending := range c.pending {
		if pending.generation != run.generation {
			continue
		}
		if pending.refresh.SourceSequence > admissionSequence {
			admissionSequence = pending.refresh.SourceSequence
		}
		if pending.refresh.Watermark > admissionWatermark {
			admissionWatermark = pending.refresh.Watermark
		}
		for key, revision := range pending.revisions {
			if revision > admissionRevisions[key] {
				admissionRevisions[key] = revision
			}
		}
	}
	if message.Tenant != scope.Tenant || message.Projection != scope.Projection || message.SourceSequence <= admissionSequence || message.Watermark < admissionWatermark {
		c.mu.Unlock()
		c.reject(run, EventRejected)
		return
	}
	allowed := make(map[string]struct{}, len(scope.Subjects))
	for _, subject := range scope.Subjects {
		allowed[subject.String()] = struct{}{}
	}
	revisions := make(map[string]uint64, len(message.Items))
	job := Refresh{Projection: scope.Projection, SourceSequence: message.SourceSequence, Watermark: message.Watermark, Subjects: make([]values.EntityRef, len(message.Items))}
	for i, item := range message.Items {
		key := item.Subject.String()
		if _, ok := allowed[key]; !ok || item.Revision <= admissionRevisions[key] {
			c.mu.Unlock()
			c.reject(run, EventRejected)
			return
		}
		job.Subjects[i] = item.Subject
		revisions[key] = item.Revision
	}
	c.nextJobID++
	queuedJob := refreshJob{id: c.nextJobID, generation: run.generation, refresh: job, revisions: revisions, ready: make(chan struct{})}
	select {
	case run.jobs <- queuedJob:
		c.pending = append(c.pending, queuedJob)
		c.accepted++
		queued := len(run.jobs)
		// The observer queue sees acceptance before the worker can publish a
		// completion, even when the refetch closure returns immediately.
		c.observe(Event{Kind: EventAccepted, Items: len(message.Items), QueueLength: queued, SourceSequence: message.SourceSequence})
		close(queuedJob.ready)
		c.mu.Unlock()
	case <-run.ctx.Done():
		c.mu.Unlock()
	default:
		c.queueFull++
		c.rejected++
		queued := len(run.jobs)
		c.mu.Unlock()
		c.observe(Event{Kind: EventQueueFull, Items: len(message.Items), QueueLength: queued})
	}
}

func (c *Client) reject(run *runState, kind EventKind) {
	c.mu.Lock()
	if c.run != run {
		c.mu.Unlock()
		return
	}
	c.rejected++
	queued := len(run.jobs)
	c.mu.Unlock()
	// A rejected event intentionally carries no attacker-controlled sequence.
	c.observe(Event{Kind: kind, QueueLength: queued})
}
