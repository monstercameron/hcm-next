package schedulingstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/schedulingstore"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/appointment"
	"github.com/monstercameron/hcm-next/internal/domains/availability"
	"github.com/monstercameron/hcm-next/internal/domains/clock"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, key)
	return id
}

func ref(tenant values.TenantId, kind values.Kind) values.EntityRef {
	return values.EntityRef{Tenant: tenant, Kind: kind, Id: uuid.NewString()}
}

func availabilityKey(id string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(id)).String()
}

func requirement(t *testing.T, tenant values.TenantId, id string, revision uint64) appointment.Requirement {
	t.Helper()
	start := values.NewInstant(time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC))
	end := values.NewInstant(time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC))
	window, err := values.NewInstantInterval(start, end)
	if err != nil {
		t.Fatal(err)
	}
	in := appointment.Requirement{
		ID: id, Version: "v1", Revision: revision, PurposeKind: appointment.PurposeInterview,
		ParticipantRefs: []values.EntityRef{ref(tenant, "worker")}, Duration: 30 * time.Minute,
		WindowInterval: window, LocationClass: appointment.LocationVirtual,
		RequiredResources: []appointment.ResourceRequirement{{ResourceTypeRef: ref(tenant, "resource_type"), Count: 1}},
		QualificationRefs: []values.EntityRef{ref(tenant, "qualification")}, PrivacyClass: "candidate-confidential",
		CancellationRules: appointment.CancellationRules{Kind: appointment.CancellationAllowed, NoShow: appointment.NoShowReview},
		State:             "DRAFT",
	}
	prepared, err := appointment.NewAppointmentRequirement(in)
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func resource(id string, revision uint64) appointment.ResourceType {
	return appointment.ResourceType{ID: id, Version: "v1", Revision: revision, Name: "video room", Kind: appointment.ResourceCapability, Qualification: "video", PrivacyClass: "internal", CapacityMode: appointment.CapacityExclusive}
}

func workerAvailability(tenant values.TenantId, id string, revision uint64) availability.WorkerAvailability {
	worker, employment := ref(tenant, "worker"), ref(tenant, "employment")
	start := values.NewInstant(time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC))
	end := values.NewInstant(time.Date(2026, 9, 10, 17, 0, 0, 0, time.UTC))
	interval, _ := values.NewInstantInterval(start, end)
	authority := ref(tenant, "authority")
	sourceRevision, _ := values.NewSequenceRevision("authority.availability", 1)
	revisionToken, _ := values.NewSequenceRevision("availability."+id, revision)
	return availability.WorkerAvailability{
		AvailabilityID: values.EntityRef{Tenant: tenant, Kind: "availability", Id: availabilityKey(id)}, Revision: revisionToken,
		Worker: worker, Employment: employment, Scope: availability.WorkerScope, State: availability.Available,
		Reason: "staffed", Effective: interval,
		Source: availability.Source{Authority: authority, Revision: sourceRevision, TimezoneID: "UTC", CalendarRef: "calendar:standard"},
	}
}

func registration(id, version string) clock.Registration {
	return clock.Registration{ID: id, Version: version, OwnerRef: uuid.NewString(), LocationRef: uuid.NewString(), ClockTrustPolicy: `{}`, OfflinePolicy: `{}`, ReplayPolicy: `{}`, SignaturePolicy: `{}`, FirmwarePolicy: `{}`, CertificatePolicy: `{}`, RetentionPolicy: `{}`}
}

func TestTodo_PERSIST_SCHEDULING_001(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "scheduling-primary")
	tenant := values.TenantId(tenantID.String())
	store := schedulingstore.New(db.Conn)
	req := requirement(t, tenant, "requirement-primary", 1)
	if err := store.PutRequirement(context.Background(), tenant, req); err != nil {
		t.Fatal(err)
	}
	if err := store.PutResourceType(context.Background(), tenant, resource("resource-primary", 1)); err != nil {
		t.Fatal(err)
	}
	if err := store.PutWorkerAvailability(context.Background(), tenant, workerAvailability(tenant, "availability-primary", 1)); err != nil {
		t.Fatal(err)
	}
	if err := store.PutRegistration(context.Background(), tenant, registration("device-primary", "v1")); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"appointment_requirement", "resource_type", "worker_availability", "time_device_registration"} {
		var count int
		if err := db.QueryRow(context.Background(), `SELECT count(*) FROM `+table+` WHERE tenant_id=$1`, tenantID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s count = %d, want 1", table, count)
		}
	}
}

func TestTodo_PERSIST_SCHEDULING_001_Fault(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "scheduling-fault")
	tenant := values.TenantId(tenantID.String())
	store := schedulingstore.New(db.Conn)
	req := requirement(t, tenant, "requirement-fault", 1)
	if err := store.PutRequirement(context.Background(), tenant, req); err != nil {
		t.Fatal(err)
	}
	if err := store.PutRequirement(context.Background(), tenant, req); !errors.Is(err, schedulingstore.ErrDuplicate) {
		t.Fatalf("duplicate = %v", err)
	}
	stale := requirement(t, tenant, "requirement-fault", 3)
	if err := store.PutRequirement(context.Background(), tenant, stale, 1); !errors.Is(err, schedulingstore.ErrVersionConflict) {
		t.Fatalf("stale = %v", err)
	}
	if err := store.PutResourceType(context.Background(), tenant, resource("resource-fault", 1)); err != nil {
		t.Fatal(err)
	}
	if err := store.PutResourceType(context.Background(), tenant, resource("resource-fault", 1)); !errors.Is(err, schedulingstore.ErrDuplicate) {
		t.Fatalf("duplicate resource = %v", err)
	}
	availability := workerAvailability(tenant, "availability-fault", 1)
	if err := store.PutWorkerAvailability(context.Background(), tenant, availability); err != nil {
		t.Fatal(err)
	}
	if err := store.PutWorkerAvailability(context.Background(), tenant, availability); !errors.Is(err, schedulingstore.ErrDuplicate) {
		t.Fatalf("duplicate availability = %v", err)
	}
	device := registration("device-fault", "v1")
	if err := store.PutRegistration(context.Background(), tenant, device); err != nil {
		t.Fatal(err)
	}
	if err := store.PutRegistration(context.Background(), tenant, device); !errors.Is(err, schedulingstore.ErrDuplicate) || schedulingstore.CodeOf(err) != schedulingstore.CodeDuplicateDevice {
		t.Fatalf("duplicate registration = %v, code=%s", err, schedulingstore.CodeOf(err))
	}
}

func TestTodo_PERSIST_SCHEDULING_001_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "scheduling-integration")
	tenant := values.TenantId(tenantID.String())
	store := schedulingstore.New(db.Conn)
	if err := store.PutRequirement(context.Background(), tenant, requirement(t, tenant, "requirement-integration", 1)); err != nil {
		t.Fatal(err)
	}
	if err := store.PutResourceType(context.Background(), tenant, resource("resource-integration", 1)); err != nil {
		t.Fatal(err)
	}
	availability := workerAvailability(tenant, "availability-integration", 1)
	if err := store.PutWorkerAvailability(context.Background(), tenant, availability); err != nil {
		t.Fatal(err)
	}
	device := registration("device-integration", "v1")
	if err := store.PutRegistration(context.Background(), tenant, device); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListRequirementVersions(context.Background(), tenant, "requirement-integration")
	if err != nil || len(rows) != 1 {
		t.Fatalf("requirement rows = %d, err = %v", len(rows), err)
	}
	loaded, err := store.GetResourceType(context.Background(), tenant, "resource-integration", 1)
	if err != nil || loaded.ID != "resource-integration" {
		t.Fatalf("resource = %+v, err = %v", loaded, err)
	}
	loadedAvailability, err := store.GetWorkerAvailability(context.Background(), tenant, availabilityKey("availability-integration"), 1)
	if err != nil || loadedAvailability.AvailabilityID.Id != availabilityKey("availability-integration") {
		t.Fatalf("availability = %+v, err = %v", loadedAvailability, err)
	}
	loadedDevice, err := store.GetRegistration(context.Background(), tenant, device.ID, device.Version)
	if err != nil || loadedDevice.ID != device.ID || loadedDevice.ClockTrustPolicy != device.ClockTrustPolicy {
		t.Fatalf("device = %+v, err = %v", loadedDevice, err)
	}
}

func appTx(t *testing.T, db *pgtest.DB, tenant uuid.UUID) dbport.Tx {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
		t.Fatal(err)
	}
	return tx
}

func TestTodo_PERSIST_SCHEDULING_001_Security(t *testing.T) {
	db := pgtest.New(t)
	tenantA := insertTenant(t, db, "scheduling-security-a")
	tenantB := insertTenant(t, db, "scheduling-security-b")
	store := schedulingstore.New(db.Conn)
	for _, tenantID := range []uuid.UUID{tenantA, tenantB} {
		tenant := values.TenantId(tenantID.String())
		suffix := tenantID.String()[:8]
		if err := store.PutRequirement(context.Background(), tenant, requirement(t, tenant, "requirement-"+suffix, 1)); err != nil {
			t.Fatal(err)
		}
		if err := store.PutResourceType(context.Background(), tenant, resource("resource-"+suffix, 1)); err != nil {
			t.Fatal(err)
		}
		if err := store.PutWorkerAvailability(context.Background(), tenant, workerAvailability(tenant, "availability-"+suffix, 1)); err != nil {
			t.Fatal(err)
		}
		if err := store.PutRegistration(context.Background(), tenant, registration("device-"+suffix, "v1")); err != nil {
			t.Fatal(err)
		}
	}
	tx := appTx(t, db, tenantA)
	defer tx.Rollback(context.Background())
	for _, table := range []string{"appointment_requirement", "resource_type", "worker_availability", "time_device_registration"} {
		var count int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("tenant A sees %d rows in %s, want 1", count, table)
		}
	}
}

func TestTodo_PERSIST_SCHEDULING_001_Recovery(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "scheduling-recovery")
	tenant := values.TenantId(tenantID.String())
	store := schedulingstore.New(db.Conn)
	if err := store.PutRequirement(context.Background(), tenant, requirement(t, tenant, "recovered", 1)); err != nil {
		t.Fatal(err)
	}
	if err := store.PutResourceType(context.Background(), tenant, resource("recovered-resource", 1)); err != nil {
		t.Fatal(err)
	}
	if err := store.PutWorkerAvailability(context.Background(), tenant, workerAvailability(tenant, "recovered-availability", 1)); err != nil {
		t.Fatal(err)
	}
	device := registration("recovered-device", "v1")
	if err := store.PutRegistration(context.Background(), tenant, device); err != nil {
		t.Fatal(err)
	}
	fresh := db.NewConn(t)
	freshStore := schedulingstore.New(fresh)
	loaded, err := freshStore.GetRequirement(context.Background(), tenant, "recovered", 1)
	if err != nil || loaded.ID != "recovered" {
		t.Fatalf("recovered = %+v, err = %v", loaded, err)
	}
	if loaded, err := freshStore.GetResourceType(context.Background(), tenant, "recovered-resource", 1); err != nil || loaded.ID != "recovered-resource" {
		t.Fatalf("recovered resource = %+v, err = %v", loaded, err)
	}
	if loaded, err := freshStore.GetWorkerAvailability(context.Background(), tenant, availabilityKey("recovered-availability"), 1); err != nil || loaded.AvailabilityID.Id != availabilityKey("recovered-availability") {
		t.Fatalf("recovered availability = %+v, err = %v", loaded, err)
	}
	if loaded, err := freshStore.GetRegistration(context.Background(), tenant, device.ID, device.Version); err != nil || loaded.ID != device.ID {
		t.Fatalf("recovered device = %+v, err = %v", loaded, err)
	}
}

func TestTodo_PERSIST_SCHEDULING_001_Mutation(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "scheduling-mutation")
	tenant := values.TenantId(tenantID.String())
	if err := schedulingstore.New(db.Conn).PutRequirement(context.Background(), tenant, requirement(t, tenant, "immutable", 1)); err != nil {
		t.Fatal(err)
	}
	store := schedulingstore.New(db.Conn)
	if err := store.PutResourceType(context.Background(), tenant, resource("immutable-resource", 1)); err != nil {
		t.Fatal(err)
	}
	if err := store.PutWorkerAvailability(context.Background(), tenant, workerAvailability(tenant, "immutable-availability", 1)); err != nil {
		t.Fatal(err)
	}
	if err := store.PutRegistration(context.Background(), tenant, registration("immutable-device", "v1")); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"appointment_requirement", "resource_type", "worker_availability", "time_device_registration"} {
		var rowID uuid.UUID
		if err := db.QueryRow(context.Background(), `SELECT row_id FROM `+table+` WHERE tenant_id=$1 LIMIT 1`, tenantID).Scan(&rowID); err != nil {
			t.Fatal(err)
		}
		if err := db.ExecErr(`UPDATE `+table+` SET row_id=row_id WHERE tenant_id=$1 AND row_id=$2`, tenantID, rowID); err == nil {
			t.Fatalf("%s update was accepted", table)
		}
		if err := db.ExecErr(`DELETE FROM `+table+` WHERE tenant_id=$1 AND row_id=$2`, tenantID, rowID); err == nil {
			t.Fatalf("%s delete was accepted", table)
		}
	}
}

var _ interface {
	PutRequirement(context.Context, values.TenantId, appointment.Requirement, ...uint64) error
} = (*schedulingstore.Store)(nil)

var _ *pgxadapter.Conn
