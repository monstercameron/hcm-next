package cell

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transporthumanwork "github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// workQueueReader adapts the application-owned work-item queue reader to the
// transport's string-ID port. Like workflowReader it owns no queries and no
// authorization decisions; it only moves identifiers and translates the
// store's not-found refusal into the transport's non-disclosing sentinel.
type workQueueReader struct {
	source app.WorkItemQueueReader
}

var _ transporthumanwork.Reader = workQueueReader{}

func newWorkQueueReader(source app.WorkItemQueueReader) transporthumanwork.Reader {
	if source == nil {
		return nil
	}
	return workQueueReader{source: source}
}

func (r workQueueReader) ListQueue(ctx context.Context, tenant, principal string, now time.Time) ([]workitem.WorkItem, error) {
	return r.source.ListWorkItemQueue(ctx, values.TenantId(tenant), principal, now)
}

func (r workQueueReader) LoadItem(ctx context.Context, tenant, workItemID string) (workitem.WorkItem, error) {
	id, err := runtime.ParseUUID(workItemID)
	if err != nil {
		return workitem.WorkItem{}, transporthumanwork.ErrNotFound
	}
	item, err := r.source.ReadWorkItem(ctx, values.TenantId(tenant), id)
	if err != nil {
		if workitem.CodeOf(err) == workitem.CodeWorkItemNotFound {
			return workitem.WorkItem{}, transporthumanwork.ErrNotFound
		}
		return workitem.WorkItem{}, err
	}
	return item, nil
}
