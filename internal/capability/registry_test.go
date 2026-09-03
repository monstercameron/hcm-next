package capability

import "testing"

func TestRegistry_NewRegistryEmpty(t *testing.T) {
	r := NewRegistry()
	if len(r.List()) != 0 {
		t.Fatalf("expected empty got %d", len(r.List()))
	}
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	def := bootstrapDefinition("hcmnext.test.reg", "test", []string{"worker"})
	if err := r.Register(def, bootstrapEcho(def.ID)); err != nil {
		t.Fatalf("register: %v", err)
	}
	rec, ok := r.Lookup(def.Key())
	if !ok {
		t.Fatal("not found")
	}
	if rec.Definition.ID != def.ID {
		t.Fatalf("id %s", rec.Definition.ID)
	}
	if rec.Digest == "" {
		t.Fatal("empty digest")
	}
}

func TestRegistry_DuplicateRegisterFails(t *testing.T) {
	r := NewRegistry()
	def := bootstrapDefinition("hcmnext.test.dup", "test", []string{"worker"})
	_ = r.Register(def, bootstrapEcho(def.ID))
	err := r.Register(def, bootstrapEcho(def.ID))
	if err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestRegistry_DeprecateAndRetire(t *testing.T) {
	r := NewRegistry()
	def := bootstrapDefinition("hcmnext.test.life", "test", []string{"worker"})
	_ = r.Register(def, bootstrapEcho(def.ID))
	if err := r.Deprecate(def.Key()); err != nil {
		t.Fatalf("deprecate: %v", err)
	}
	rec, _ := r.Lookup(def.Key())
	if rec.Status != StatusDeprecated {
		t.Fatalf("status %s", rec.Status)
	}
	if err := r.Retire(def.Key()); err != nil {
		t.Fatalf("retire: %v", err)
	}
	rec, _ = r.Lookup(def.Key())
	if rec.Status != StatusRetired {
		t.Fatalf("status %s", rec.Status)
	}
}

func TestRegistry_VersionsAndLatestActive(t *testing.T) {
	r, _ := NewBootstrapRegistry()
	vers := r.Versions("hcmnext.people.explain_worker_state")
	if len(vers) != 1 {
		t.Fatalf("versions %d", len(vers))
	}
	latest, ok := r.LatestActive("hcmnext.people.explain_worker_state")
	if !ok || latest.Status != StatusActive {
		t.Fatalf("latest %+v %v", latest, ok)
	}
}
