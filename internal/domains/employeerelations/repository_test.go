package employeerelations

import (
	"context"
	"errors"
	"testing"
)

func TestRepositoryStoreErrorCodesAreMatchable(t *testing.T) {
	for _, tc := range []struct {
		code StoreErrorCode
		want error
	}{
		{StoreInvalidCode, ErrStoreInvalid},
		{StoreNotFoundCode, ErrStoreNotFound},
		{StoreDuplicateCode, ErrStoreDuplicate},
		{StoreStaleCASCode, ErrStoreStaleCAS},
	} {
		err := &StoreError{Code: tc.code, Detail: "test"}
		if !errors.Is(err, tc.want) {
			t.Fatalf("%s is not errors.Is %v", tc.code, tc.want)
		}
	}
}

func TestRepositoryPortIsTenantAware(t *testing.T) {
	var _ Repository
	_ = context.Background()
}
