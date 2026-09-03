package model_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/intent/model"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var update = flag.Bool("update", false, "rewrite the checked-in golden vectors")

func goldenText(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (regenerate with -update): %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden %s drifted\n got:\n%s\nwant:\n%s", path, got, want)
	}
}

func goldenJSON(t *testing.T, name string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden %s: %v", name, err)
	}
	goldenText(t, name, append(b, '\n'))
}

func mustRegistry(t *testing.T) *model.Registry {
	t.Helper()
	reg, err := model.Catalog()
	if err != nil {
		t.Fatalf("compile catalog: %v", err)
	}
	return reg
}

func instant(y int, m time.Month, d int) values.Instant {
	return values.NewInstant(time.Date(y, m, d, 0, 0, 0, 0, time.UTC))
}

func mustOpenInterval(t *testing.T, y int, m time.Month, d int) values.EffectiveInterval {
	t.Helper()
	iv, err := values.NewOpenInstantInterval(instant(y, m, d))
	if err != nil {
		t.Fatalf("build interval: %v", err)
	}
	return iv
}

func mustInterval(t *testing.T, fy int, fm time.Month, fd int, ty int, tm time.Month, td int) values.EffectiveInterval {
	t.Helper()
	iv, err := values.NewInstantInterval(instant(fy, fm, fd), instant(ty, tm, td))
	if err != nil {
		t.Fatalf("build interval: %v", err)
	}
	return iv
}

func mustRecordedAt(t *testing.T, y int, m time.Month, d int) values.RecordedAt {
	t.Helper()
	r, err := values.NewRecordedAt(instant(y, m, d))
	if err != nil {
		t.Fatalf("build recorded at: %v", err)
	}
	return r
}

func mustKnownAt(t *testing.T, y int, m time.Month, d int) values.KnownAt {
	t.Helper()
	k, err := values.NewKnownAt(instant(y, m, d))
	if err != nil {
		t.Fatalf("build known at: %v", err)
	}
	return k
}
