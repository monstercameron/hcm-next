// Package database owns the provider-neutral PostgreSQL substrate contract.
// It validates the properties an adapter must provision without importing a
// database driver or making a network call.
package database

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const contractVersion = 1

// Version reports the IAC-006 contract version.
func Version() int { return contractVersion }

// Explain describes the database provisioning boundary without credentials or
// endpoint values.
func Explain() string {
	return "IAC-006 v1: private TLS PostgreSQL HA PITR pooling roles RLS and migration fencing"
}

type Role struct {
	Name       string
	CanMigrate bool
	CanRepair  bool
	CanRead    bool
	CanWrite   bool
}

// Plan is the complete set of provider-neutral database requirements.
type Plan struct {
	ClusterID            string
	CellID               string
	TLSRequired          bool
	PublicAccess         bool
	HighAvailability     bool
	ReplicaCount         int
	FailureDomains       int
	PoolMin              int
	PoolMax              int
	PoolWait             time.Duration
	PITREnabled          bool
	BackupEncrypted      bool
	BackupRetention      time.Duration
	MonitoringEnabled    bool
	RLSEnabled           bool
	MigrationLockName    string
	MigrationLockTimeout time.Duration
	Roles                []Role
}

type Violation struct {
	Field  string
	Code   string
	Detail string
}

type MigrationFence struct {
	LockName     string
	WriterID     string
	Epoch        uint64
	CurrentOwner string
	CurrentEpoch uint64
}

type FenceDecision struct {
	Allowed bool
	Code    string
	Detail  string
}

type RestoreVerification struct {
	LedgerHeadsValid      bool
	ForeignKeysValid      bool
	MigrationJournalValid bool
	RuntimeLeasesValid    bool
	RLSValidated          bool
}

func (v RestoreVerification) Passed() bool {
	return v.LedgerHeadsValid && v.ForeignKeysValid && v.MigrationJournalValid && v.RuntimeLeasesValid && v.RLSValidated
}

var ErrInvalidPlan = errors.New("database: invalid provisioning plan")

func Validate(p Plan) []Violation {
	var out []Violation
	need := func(field, code, detail string, bad bool) {
		if bad {
			out = append(out, Violation{Field: field, Code: code, Detail: detail})
		}
	}
	need("cluster_id", "MISSING_CLUSTER", "cluster id is required", strings.TrimSpace(p.ClusterID) == "")
	need("cell_id", "MISSING_CELL", "cell id is required", strings.TrimSpace(p.CellID) == "")
	need("tls", "TLS_REQUIRED", "database connections must require TLS", !p.TLSRequired)
	need("public_access", "PUBLIC_ACCESS", "database must not be publicly reachable", p.PublicAccess)
	need("high_availability", "HA_REQUIRED", "database must have high availability", !p.HighAvailability)
	need("replica_count", "REPLICAS_REQUIRED", "HA requires at least two replicas", p.ReplicaCount < 2)
	need("failure_domains", "FAILURE_DOMAINS_REQUIRED", "HA replicas must span at least two failure domains", p.FailureDomains < 2)
	need("pool_max", "POOL_REQUIRED", "pool max must be positive and at least pool min", p.PoolMax <= 0 || p.PoolMin < 0 || p.PoolMin > p.PoolMax)
	need("pool_wait", "POOL_WAIT_REQUIRED", "pool wait must be positive", p.PoolWait <= 0)
	need("pitr", "PITR_REQUIRED", "point-in-time recovery must be enabled", !p.PITREnabled)
	need("backup_encrypted", "BACKUP_ENCRYPTION_REQUIRED", "backups must be encrypted", !p.BackupEncrypted)
	need("backup_retention", "BACKUP_RETENTION_REQUIRED", "backup retention must be positive", p.BackupRetention <= 0)
	need("monitoring", "MONITORING_REQUIRED", "database monitoring must be enabled", !p.MonitoringEnabled)
	need("rls", "RLS_REQUIRED", "row-level security must be enabled", !p.RLSEnabled)
	need("migration_lock_name", "MIGRATION_LOCK_REQUIRED", "one migration lock name is required", strings.TrimSpace(p.MigrationLockName) == "")
	need("migration_lock_timeout", "MIGRATION_LOCK_TIMEOUT_REQUIRED", "migration lock timeout must be positive", p.MigrationLockTimeout <= 0)
	if len(p.Roles) == 0 {
		out = append(out, Violation{Field: "roles", Code: "ROLES_REQUIRED", Detail: "least-privilege database roles are required"})
	}
	roleNames := make(map[string]bool, len(p.Roles))
	for _, role := range p.Roles {
		if strings.TrimSpace(role.Name) == "" || roleNames[role.Name] {
			out = append(out, Violation{Field: "roles", Code: "ROLE_ID_INVALID", Detail: "database role names must be non-empty and unique"})
		}
		roleNames[role.Name] = true
	}
	if !roleNames["app"] {
		out = append(out, Violation{Field: "roles.app", Code: "APP_ROLE_REQUIRED", Detail: "an application role is required"})
	}
	if !roleNames["migrator"] {
		out = append(out, Violation{Field: "roles.migrator", Code: "MIGRATOR_ROLE_REQUIRED", Detail: "a separate migration role is required"})
	}
	if roleNames["app"] && roleNames["migrator"] {
		for _, role := range p.Roles {
			if role.Name == "app" && role.CanMigrate {
				out = append(out, Violation{Field: "roles.app", Code: "APP_MIGRATION_PRIVILEGE", Detail: "application role must not migrate"})
			}
			if role.Name == "migrator" && !role.CanMigrate {
				out = append(out, Violation{Field: "roles.migrator", Code: "MIGRATOR_PRIVILEGE_MISSING", Detail: "migration role must be able to migrate"})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Field != out[j].Field {
			return out[i].Field < out[j].Field
		}
		return out[i].Code < out[j].Code
	})
	return out
}

func Check(p Plan) error {
	if violations := Validate(p); len(violations) != 0 {
		v := violations[0]
		return fmt.Errorf("%w: %s %s: %s", ErrInvalidPlan, v.Code, v.Field, v.Detail)
	}
	return nil
}

// AdmitMigration allows one writer for one named lock and epoch. The caller
// supplies current lock state, so the function is deterministic and safe to
// use by SQL, file-lock, or orchestration adapters.
func AdmitMigration(p Plan, fence MigrationFence) FenceDecision {
	if err := Check(p); err != nil {
		return FenceDecision{Code: "INVALID_PLAN", Detail: err.Error()}
	}
	if strings.TrimSpace(fence.WriterID) == "" || fence.Epoch == 0 || fence.LockName != p.MigrationLockName {
		return FenceDecision{Code: "MIGRATION_FENCE_DENIED", Detail: "writer, epoch, and the declared lock are required"}
	}
	if fence.CurrentOwner != "" && fence.CurrentOwner != fence.WriterID {
		return FenceDecision{Code: "MIGRATION_LOCK_HELD", Detail: "another migration writer holds the lock"}
	}
	if fence.CurrentEpoch != 0 && fence.CurrentEpoch != fence.Epoch {
		return FenceDecision{Code: "MIGRATION_EPOCH_STALE", Detail: "migration writer epoch is stale"}
	}
	return FenceDecision{Allowed: true, Code: "MIGRATION_ALLOWED", Detail: "single migration writer admitted for the declared epoch"}
}

// VerifyRestore is the database-side portion of restore acceptance. It never
// clears the application fence; the recovery owner combines this result with
// hold, deletion, and artifact conformance.
func VerifyRestore(v RestoreVerification) error {
	if !v.Passed() {
		return errors.New("database: restore conformance is incomplete")
	}
	return nil
}
