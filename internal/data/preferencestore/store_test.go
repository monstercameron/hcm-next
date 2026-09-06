package preferencestore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/experience/preferences"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestStorePersistsPrincipalSettingsAndOrganizationAppearanceWithCAS(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-test','Test','ACTIVE',$3)`, tenantID, "prefs-test", time.Now().UTC())
	store := New(db.Conn, func(values.TenantId) uuid.UUID { return tenantID })
	ctx := context.Background()

	const organizationNorth = "org:test:north"
	const organizationSouth = "org:test:south"
	snapshot, err := store.Load(ctx, values.TenantId("prefs-test"), organizationNorth, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.User.Version != 0 || snapshot.User.Tables[preferences.TablePeople].PageSize != 20 {
		t.Fatalf("unexpected defaults: %+v", snapshot.User)
	}

	user := snapshot.User
	user.Locale = "de-DE"
	user.Tables[preferences.TablePeople] = preferences.TablePreferences{PageSize: 50, Filters: map[string]string{"team": "Care Operations"}, Sort: "name", Direction: "asc"}
	user, err = store.SaveUser(ctx, values.TenantId("prefs-test"), "alice", user)
	if err != nil || user.Version != 1 {
		t.Fatalf("save user: version=%d err=%v", user.Version, err)
	}
	stale := user
	stale.Version = 0
	if _, err = store.SaveUser(ctx, values.TenantId("prefs-test"), "alice", stale); !errors.Is(err, preferences.ErrVersionConflict) {
		t.Fatalf("stale save err=%v", err)
	}

	theme := preferences.DefaultSnapshot().Theme
	theme.Theme.Palette = "ocean"
	theme, err = store.SaveTheme(ctx, values.TenantId("prefs-test"), organizationNorth, "alice", theme)
	if err != nil || theme.Version != 1 {
		t.Fatalf("save theme: version=%d err=%v", theme.Version, err)
	}
	staleTheme := theme
	staleTheme.Version = 0
	if _, err = store.SaveTheme(ctx, values.TenantId("prefs-test"), organizationNorth, "alice", staleTheme); !errors.Is(err, preferences.ErrVersionConflict) {
		t.Fatalf("stale organization appearance save err=%v", err)
	}

	loaded, err := store.Load(ctx, values.TenantId("prefs-test"), organizationNorth, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.User.Locale != "de-DE" || loaded.User.Tables[preferences.TablePeople].PageSize != 50 || loaded.Theme.Palette != "ocean" {
		t.Fatalf("round trip lost preferences: %+v", loaded)
	}

	// A second principal in the same organization receives the shared theme
	// but not Alice's personal locale, navigation, or table configuration.
	bob, err := store.Load(ctx, values.TenantId("prefs-test"), organizationNorth, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if bob.Theme.Palette != "ocean" || bob.Theme.OrganizationScopeID != organizationNorth {
		t.Fatalf("organization appearance was not shared: %+v", bob.Theme)
	}
	if bob.User.Locale != "" || bob.User.Version != 0 || bob.User.Tables[preferences.TablePeople].PageSize != 20 {
		t.Fatalf("Alice's user settings leaked to Bob: %+v", bob.User)
	}

	// A principal in a different organization under the same tenant receives
	// neither organization's appearance; the default remains honest.
	carol, err := store.Load(ctx, values.TenantId("prefs-test"), organizationSouth, "carol")
	if err != nil {
		t.Fatal(err)
	}
	if carol.Theme.Palette != preferences.DefaultSnapshot().Theme.Palette || carol.Theme.Version != 0 || carol.Theme.OrganizationScopeID != organizationSouth {
		t.Fatalf("organization appearance crossed scopes: %+v", carol.Theme)
	}
	carolTheme := carol.Theme
	carolTheme.Theme.Palette = "violet"
	carolTheme, err = store.SaveTheme(ctx, values.TenantId("prefs-test"), organizationSouth, "carol", carolTheme)
	if err != nil || carolTheme.Version != 1 {
		t.Fatalf("independent organization appearance stream: version=%d err=%v", carolTheme.Version, err)
	}
	northAgain, err := store.Load(ctx, values.TenantId("prefs-test"), organizationNorth, "alice")
	if err != nil || northAgain.Theme.Palette != "ocean" || northAgain.Theme.Version != 1 {
		t.Fatalf("south organization changed north appearance: theme=%+v err=%v", northAgain.Theme, err)
	}

	bob.User.Locale = "ar"
	if _, err = store.SaveUser(ctx, values.TenantId("prefs-test"), "bob", bob.User); err != nil {
		t.Fatal(err)
	}
	aliceAgain, err := store.Load(ctx, values.TenantId("prefs-test"), organizationNorth, "alice")
	if err != nil || aliceAgain.User.Locale != "de-DE" {
		t.Fatalf("Bob's settings changed Alice: locale=%q err=%v", aliceAgain.User.Locale, err)
	}

	used, err := store.RecordWorkflowUse(ctx, values.TenantId("prefs-test"), "alice", "promotion")
	if err != nil || used.WorkflowUses["promotion"] != 1 {
		t.Fatalf("record workflow use: %+v err=%v", used.WorkflowUses, err)
	}
}

func TestStoreRejectsMissingAuthenticatedCoordinates(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-test','Test','ACTIVE',$3)`, tenantID, "prefs-invalid-test", time.Now().UTC())
	store := New(db.Conn, func(values.TenantId) uuid.UUID { return tenantID })
	tenant := values.TenantId("prefs-invalid-test")

	if _, err := store.Load(context.Background(), tenant, "", "alice"); !errors.Is(err, preferences.ErrInvalid) {
		t.Fatalf("load without organization scope err=%v", err)
	}
	if _, err := store.Load(context.Background(), tenant, "org:test:north", ""); !errors.Is(err, preferences.ErrInvalid) {
		t.Fatalf("load without principal err=%v", err)
	}
	if _, err := store.SaveTheme(context.Background(), tenant, "", "alice", preferences.DefaultSnapshot().Theme); !errors.Is(err, preferences.ErrInvalid) {
		t.Fatalf("save appearance without organization scope err=%v", err)
	}
}
