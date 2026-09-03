package app

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/hcm-next/internal/connectivity"
	"github.com/monstercameron/hcm-next/internal/connectivity/observe"
	"github.com/monstercameron/hcm-next/internal/domains/dataops"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// externalFieldMapping is the incumbent's field vocabulary projected onto the
// comparison vocabulary.
//
// It is a mapping profile, not a decoder: the connectivity plane observes and
// hands back strings, and turning "job_code" into the governed field
// assignment.job_code is a semantic decision this cell owns. A record key that
// is not in this table is not observed at all, which is what keeps a provider
// that starts returning extra columns from silently widening the projection.
var externalFieldMapping = map[string]dataops.FieldID{
	"worker_number":    dataops.FieldID(people.FieldWorkerNumber),
	"lifecycle_status": dataops.FieldID(people.FieldLifecycleStatus),
	"legal_name":       dataops.FieldID(people.FieldLegalName),
	"preferred_name":   dataops.FieldID(people.FieldPreferredName),
	"employment_id":    dataops.FieldID(people.FieldEmploymentID),
	"worker_type":      dataops.FieldID(people.FieldWorkerType),
	"hire_date":        dataops.FieldID(people.FieldHireDate),
	"job_code":         dataops.FieldID(people.FieldJobCode),
	"grade":            dataops.FieldID(people.FieldGrade),
	"org_unit":         dataops.FieldID(people.FieldOrgUnit),
	"position_id":      dataops.FieldID(people.FieldPositionID),
	"location":         dataops.FieldID(people.FieldLocation),
	"pay_zone":         dataops.FieldID(people.FieldPayZone),
	"fte":              dataops.FieldID(people.FieldFTE),
}

// externalEmploymentStatus is the one comparison field the incumbent reports
// under a name the mapping cannot reach directly: the connector publishes a
// single lifecycle_status, and the employment status is the same observation
// read under its employment meaning.
const externalEmploymentStatus = "lifecycle_status"

// ExternalObservations is the external half of every comparison: a
// [dataops.ObservationReader] over a read-only [connectivity.Connector].
//
// Every page it reads is recorded as immutable observation evidence before it
// is handed to the comparison, which is what makes a finding traceable back to
// the exact page, snapshot and cursor it rested on. Nothing here can write:
// the Connector interface has no method that could, and the observation store
// only appends evidence about what was read.
type ExternalObservations struct {
	connector  connectivity.Connector
	connection *connectivity.ConnectorConnection
	store      observe.ObservationStore
	// budget is how old a page's watermark may be before it is reported as
	// stale. Zero makes every page's freshness UNKNOWN, which is the honest
	// answer for a connector that declares no budget.
	budget time.Duration
}

var _ dataops.ObservationReader = (*ExternalObservations)(nil)

// NewExternalObservations wires an observation reader over one usable
// connection.
func NewExternalObservations(
	connector connectivity.Connector,
	connection *connectivity.ConnectorConnection,
	store observe.ObservationStore,
	budget time.Duration,
) (*ExternalObservations, error) {
	switch {
	case connector == nil:
		return nil, fmt.Errorf("app: an observation reader needs a connector")
	case connection == nil:
		return nil, fmt.Errorf("app: an observation reader needs a connection")
	case store == nil:
		return nil, fmt.Errorf("app: an observation reader needs an observation store")
	}
	if err := connection.RequireUsable(); err != nil {
		return nil, err
	}
	return &ExternalObservations{connector: connector, connection: connection, store: store, budget: budget}, nil
}

// SourceRef is the observing system identifier a drift request names.
func (o *ExternalObservations) SourceRef() string { return o.connector.Descriptor().SourceRef }

// ObservationsAt implements [dataops.ObservationReader].
//
// One call is one page. The page boundary is the connector's, not the
// caller's: a request for a hundred records against a connector that publishes
// a two-record ceiling is clamped down to the ceiling rather than refused,
// because the caller is asking for a bounded traversal and the connector owns
// the bound. Reading past the ceiling would be the one thing
// [connectivity.Bounds] exists to prevent.
func (o *ExternalObservations) ObservationsAt(ctx context.Context, q dataops.ObservationQuery) (dataops.ObservationPage, error) {
	descriptor := o.connector.Descriptor()
	if q.Source != descriptor.SourceRef {
		return dataops.ObservationPage{}, fmt.Errorf(
			"app: this connection observes %q, the request asked for %q", descriptor.SourceRef, q.Source)
	}

	snapshot, err := o.connector.Snapshot(ctx, connectivity.ObjectWorker)
	if err != nil {
		return dataops.ObservationPage{}, err
	}

	cursor := connectivity.StartCursor(snapshot)
	if q.Cursor != "" {
		parsed, parseErr := connectivity.ParseCursor(q.Cursor)
		if parseErr != nil {
			return dataops.ObservationPage{}, parseErr
		}
		cursor = parsed
	}

	limit := q.Limit
	bounds := o.connection.Bounds()
	if limit <= 0 || limit > bounds.MaxPageSize {
		limit = bounds.MaxPageSize
	}

	page, err := o.connector.Read(ctx, connectivity.ReadRequest{
		Object: connectivity.ObjectWorker,
		Mode:   connectivity.ReadFull,
		Cursor: cursor,
		Limit:  limit,
	})
	if err != nil {
		return dataops.ObservationPage{}, err
	}

	// Evidence first, answer second. The observation is durable before the
	// comparison sees a single value, so a finding can always be traced back
	// to the page it rested on even if the run fails immediately afterwards.
	observation, err := observe.Record(page, observe.RecordOptions{
		TenantID:        string(q.Tenant),
		Descriptor:      descriptor,
		PageSequence:    cursor.Page + 1,
		StartCursor:     cursor,
		FreshnessBudget: o.budget,
	})
	if err != nil {
		return dataops.ObservationPage{}, err
	}
	if _, err := o.store.Append(ctx, observation); err != nil {
		return dataops.ObservationPage{}, err
	}

	retrieved, err := values.NewRecordedAt(values.NewInstant(page.RetrievedAt))
	if err != nil {
		return dataops.ObservationPage{}, fmt.Errorf("app: observation retrieval time: %w", err)
	}

	out := dataops.ObservationPage{
		Source:        descriptor.SourceRef,
		SchemaVersion: page.SchemaVersion,
		RetrievedAt:   retrieved,
		Cursor:        q.Cursor,
		Digest:        observation.ContentDigest,
	}
	if !page.Complete {
		token, tokenErr := page.NextCursor.Token()
		if tokenErr != nil {
			return dataops.ObservationPage{}, tokenErr
		}
		out.NextCursor = token
	}

	wanted := make(map[values.EntityRef]struct{}, len(q.Subjects))
	for _, s := range q.Subjects {
		wanted[s] = struct{}{}
	}
	for _, record := range page.Records {
		subject := values.EntityRef{Tenant: q.Tenant, Kind: people.KindWorker, Id: record.ExternalID}
		if _, ok := wanted[subject]; !ok {
			// A record about somebody outside the requested population is
			// dropped here rather than passed on: the comparison refuses a
			// widened projection, and it is right to.
			continue
		}
		out.Records = append(out.Records, observedRecord(subject, record, q.Fields, page.RetrievedAt))
	}
	return out, nil
}

// observedRecord projects one external record onto the comparison's observed
// side, restricted to the requested projection.
func observedRecord(
	subject values.EntityRef,
	record connectivity.Record,
	fields []dataops.FieldID,
	retrievedAt time.Time,
) dataops.ObservedRecord {
	requested := make(map[dataops.FieldID]struct{}, len(fields))
	for _, f := range fields {
		requested[f] = struct{}{}
	}

	updated := values.NewInstant(record.ObservedAt)
	if record.ObservedAt.IsZero() {
		updated = values.NewInstant(retrievedAt)
	}

	out := dataops.ObservedRecord{
		Subject:    subject,
		ExternalID: record.ExternalID,
		Exists:     true,
		Digest:     record.SourceVersion,
	}
	seen := make(map[dataops.FieldID]struct{}, len(record.Fields))
	// Iteration is over the record's sorted key list rather than the map, so
	// two reads of one page produce the same field order and therefore the
	// same canonical bytes.
	for _, key := range record.FieldKeys() {
		field, ok := externalFieldMapping[key]
		if !ok {
			continue
		}
		if _, want := requested[field]; !want {
			continue
		}
		if _, dup := seen[field]; dup {
			continue
		}
		seen[field] = struct{}{}
		out.Fields = append(out.Fields, dataops.ObservedField{
			Field:     field,
			Kind:      kindOf(field),
			Value:     values.Value(record.Fields[key]),
			UpdatedAt: updated,
		})
	}

	// The employment status is the same observation as the lifecycle status
	// read under its employment meaning. It is stated explicitly rather than
	// mapped twice, so that a connector that later publishes a distinct
	// employment status replaces one line instead of changing a shared table.
	status := dataops.FieldID(people.FieldEmploymentStatus)
	if _, want := requested[status]; want {
		if raw, ok := record.Fields[externalEmploymentStatus]; ok {
			if _, dup := seen[status]; !dup {
				out.Fields = append(out.Fields, dataops.ObservedField{
					Field:     status,
					Kind:      kindOf(status),
					Value:     values.Value(raw),
					UpdatedAt: updated,
				})
			}
		}
	}
	return out
}
