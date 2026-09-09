package quarantine_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
)

func TestErrorCodesAndMessages(t *testing.T) {
	cases := []struct {
		name string
		err  interface {
			error
			Code() string
		}
		wantCode string
	}{
		{"request invalid", quarantine.ErrRequestInvalid{Field: "Tenant", Reason: "is required"}, quarantine.CodeRequestInvalid},
		{"content too large", quarantine.ErrContentTooLarge{Limit: 100, Size: 200}, quarantine.CodeContentTooLarge},
		{"not found", quarantine.ErrNotFound{ContentID: "abc"}, quarantine.CodeNotFound},
		{"use refused", quarantine.ErrUseRefused{ContentID: "abc", State: quarantine.Rejected, Reason: "unsafe"}, quarantine.CodeUseRefused},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.err.Code() != c.wantCode {
				t.Errorf("Code() = %s, want %s", c.err.Code(), c.wantCode)
			}
			if !strings.Contains(c.err.Error(), c.wantCode) {
				t.Errorf("Error() = %q, want it to name its own code %s", c.err.Error(), c.wantCode)
			}
		})
	}

	t.Run("ErrUseRefused without a reason omits the trailing colon", func(t *testing.T) {
		err := quarantine.ErrUseRefused{ContentID: "abc", State: quarantine.Quarantined}
		if strings.Contains(err.Error(), ": :") {
			t.Errorf("Error() = %q looks malformed with an empty reason", err.Error())
		}
	})
}
