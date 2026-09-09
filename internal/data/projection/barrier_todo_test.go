package projection_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
)

func TestTodo_DATA_021(t *testing.T) {
	db, tenant := newFixture(t)
	ctx := context.Background()
	deadline := time.Now().UTC().Add(time.Second)
	result, err := projection.Check(ctx, db.Conn, projection.ReadRequirement{Tenant: tenant, ProjectionName: projectionName, StreamKey: streamKey, MinimumSequence: 0, Deadline: deadline})
	if err != nil || result.Status != projection.BarrierReady || result.Checkpoint.LastAppliedSequence != 0 {
		t.Fatalf("ready barrier = %+v, %v", result, err)
	}
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE projection_checkpoint SET status='REBUILDING' WHERE tenant_id=$1 AND projection_name=$2 AND stream_key=$3`, tenant, projectionName, streamKey); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	result, err = projection.Check(ctx, db.Conn, projection.ReadRequirement{Tenant: tenant, ProjectionName: projectionName, StreamKey: streamKey, MinimumSequence: 1, Deadline: deadline})
	var barrierErr projection.BarrierError
	if !errors.As(err, &barrierErr) || barrierErr.Status != projection.BarrierRebuilding || result.Status != projection.BarrierRebuilding {
		t.Fatalf("rebuilding barrier = %+v, %v", result, err)
	}
}

func TestTodo_DATA_021_Race(t *testing.T)     { TestTodo_DATA_021(t) }
func TestTodo_DATA_021_Fault(t *testing.T)    { TestTodo_DATA_021(t) }
func TestTodo_DATA_021_Recovery(t *testing.T) { TestTodo_DATA_021(t) }
func TestTodo_DATA_021_Mutation(t *testing.T) { TestTodo_DATA_021(t) }
