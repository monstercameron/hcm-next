package subscription_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/subscription"
)

func TestRevisionRepositoryPortIncludesMemoryRegistry(t *testing.T) {
	var _ subscription.Repository = subscription.NewRegistry()
}
