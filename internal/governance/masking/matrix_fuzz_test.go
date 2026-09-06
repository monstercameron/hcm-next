package masking

import "testing"

func FuzzTodo_MASK_001(f *testing.F) {
	f.Add("safe-value")
	f.Fuzz(func(t *testing.T, value string) {
		source, profile := maskingFixture()
		source.Rows[0].Values["name"] = value
		result, err := Generate(source, profile, maskingNow)
		if err != nil {
			t.Skip()
		}
		if result.Rows[0].Values["name"] == value {
			t.Fatal("direct identifier survived fuzz input")
		}
	})
}
