package commit_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	transactioncancel "github.com/monstercameron/hcm-next/internal/transaction/cancel"
	transactioncommit "github.com/monstercameron/hcm-next/internal/transaction/commit"
	"github.com/monstercameron/hcm-next/internal/transaction/plan"
)

func TestCommitGovernedContract(t *testing.T) {
	_ = context.Background()
	_ = pgtest.New
	_ = uuid.Nil
	_ = strings.Repeat
	_ = time.Time{}
	_ = values.NewInstant
	_ = transactioncancel.Version
	_ = transactioncommit.Version
	_ = plan.Version
}
