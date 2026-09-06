package floorplan

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/tools/uxqual/pagedef"
)

func TestTodo_WEB_003(t *testing.T) {
	r := PromotionRegistry()
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	list := pagedef.PromotionListPageDefinition()
	resolved, err := r.Resolve(list)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Floorplan.ID != "floorplan.launch" || resolved.Floorplan.Version != 1 {
		t.Fatalf("resolved floorplan = %s@%d", resolved.Floorplan.ID, resolved.Floorplan.Version)
	}
	if _, err := r.Resolve(pagedef.PageDefinition{
		PageID: "invalid", Version: 1, FloorplanRef: "floorplan.launch.v1",
		Regions:       []pagedef.Region{{ID: "not-declared", Kind: pagedef.RegionPrimary}},
		Accessibility: pagedef.Accessibility{Landmarks: []string{"main"}, LiveRegion: pagedef.LiveRegionOff},
	}); !errors.Is(err, ErrUnknownRegion) {
		t.Fatalf("unknown page region error = %v, want ErrUnknownRegion", err)
	}
	if _, ok := r.Lookup("floorplan.launch@1"); !ok {
		t.Fatal("Lookup did not resolve ref@version")
	}
	if _, ok := r.Lookup("floorplan.launch", 1); !ok {
		t.Fatal("Lookup did not resolve id, version")
	}
	if _, ok := r.Lookup("floorplan.launch.v2"); ok {
		t.Fatal("Lookup resolved an unregistered version")
	}
}

func TestTodo_WEB_003_Golden(t *testing.T) {
	r := PromotionRegistry()
	wants := map[string]string{
		"floorplan.launch@1":           "sha256:bff30447dc98d06445669a395dab08631d2e0df1caab34eaa2e01029161fbdea",
		"floorplan.intent_workspace@1": "sha256:6cfbe67acffd40e2ee444363946ab4afdc3cdf009b22abb796f6c6c8999f323b",
	}
	for ref, want := range wants {
		fp, ok := r.Lookup(ref)
		if !ok {
			t.Fatalf("missing golden floorplan %q", ref)
		}
		if fp.Digest() != want {
			t.Fatalf("%s digest = %s, want %s", ref, fp.Digest(), want)
		}
	}
}

func TestTodo_WEB_003_Browser(t *testing.T) {
	fp, ok := PromotionRegistry().Lookup("floorplan.intent_workspace.v1")
	if !ok {
		t.Fatal("Promotion intent workspace is not registered")
	}
	if len(fp.Breakpoints) != 4 || fp.Regions[0].Layout.Mode != LayoutFlow {
		t.Fatalf("incomplete responsive/browser contract: %+v", fp)
	}
}

func TestTodo_WEB_003_Conformance(t *testing.T) {
	base := promotionLaunch()
	mutations := []Floorplan{
		func() Floorplan { f := base; f.Regions = append(f.Regions, f.Regions[0]); return f }(),
		func() Floorplan { f := base; f.Regions[0].Kind = pagedef.RegionKind("sidebar"); return f }(),
		func() Floorplan { f := base; f.ResponsiveRules[0].Region = "missing"; return f }(),
	}
	for i, f := range mutations {
		if err := f.Validate(); err == nil {
			t.Fatalf("mutation %d was accepted", i)
		}
	}

	f := promotionLaunch()
	const workers = 16
	digests := make([]string, workers)
	var wg sync.WaitGroup
	for i := range digests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			digests[i] = f.Digest()
		}(i)
	}
	wg.Wait()
	for _, got := range digests[1:] {
		if got != digests[0] {
			t.Fatalf("digest changed across goroutines: %s != %s", got, digests[0])
		}
	}
}
