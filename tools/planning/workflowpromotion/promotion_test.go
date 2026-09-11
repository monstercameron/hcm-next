package workflowpromotion

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowarchetypes"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdecisions"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowregistry"
)

// testSigningSeed is a TEST-ONLY deterministic key. It signs fixture
// receipts so goldens stay stable; it must never sign real promotions.
const testSigningSeed = "4242424242424242424242424242424242424242424242424242424242424242"

func testKeys(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	seed, err := hex.DecodeString(testSigningSeed)
	if err != nil {
		t.Fatal(err)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	return priv, hex.EncodeToString(pub)
}

func completeClosure() PromotionClosure {
	return PromotionClosure{
		ExploratoryDigest: "sha256:" + strings.Repeat("a", 64),
		GraphDigest:       "sha256:" + strings.Repeat("b", 64),
		Schemas: []SchemaRef{
			{Name: "ContactPointRevision", Digest: "sha256:" + strings.Repeat("c", 64)},
		},
		Capabilities:  map[string]string{"identity-resolve": "1.4.0"},
		Behaviors:     []Behavior{{Name: "verify-endpoint", Type: "Effect"}},
		LegacyTouched: false,
		Owner:         "people-domain",
		ReviewExpiry:  "2099-01-01",
		SafeDefault:   workflowdecisions.DefaultBlock,
		Fixtures: []workflowdecisions.NegativeFixture{
			{Action: "merge-high-uncertainty", Signal: "uncertainty-high", Expect: workflowdecisions.DefaultBlock},
		},
	}
}

func TestExploratoryWorkflowPromotionRequiresExactContractClosure(t *testing.T) {
	priv, pub := testKeys(t)
	receipt, err := Promote(completeClosure(), priv, "2098-06-01")
	if err != nil {
		t.Fatalf("complete closure rejected: %v", err)
	}
	if receipt.State != "CONTRACTED" {
		t.Fatalf("state = %q, want CONTRACTED", receipt.State)
	}
	if receipt.Signature == "" {
		t.Fatal("receipt carries no signature")
	}
	ok, reason := VerifyReceipt(receipt, pub, "2098-06-01")
	if !ok {
		t.Fatalf("receipt does not verify: %s", reason)
	}

	broken := completeClosure()
	broken.Schemas = []SchemaRef{{Name: "ContactPointRevision", Digest: ""}}
	broken.Capabilities = map[string]string{"identity-resolve": "latest"}
	broken.Behaviors = []Behavior{{Name: "mystery", Type: "UNKNOWN"}}
	broken.LegacyTouched = true
	broken.Owner = ""
	broken.ReviewExpiry = "2000-01-01"
	receipt, promotionErr := Promote(broken, priv, "2098-06-01")
	if promotionErr == nil {
		t.Fatal("open closure promoted")
	}
	if receipt.Signature != "" {
		t.Fatal("failed promotion published a signed definition")
	}
	for _, code := range []string{
		"UNRESOLVED_SCHEMA", "UNVERSIONED_CAPABILITY", "UNTYPED_BEHAVIOR",
		"MISSING_PARITY_RECORD", "MISSING_OWNER", "EXPIRED_REVIEW",
	} {
		if !strings.Contains(promotionErr.Error(), code) {
			t.Errorf("closure set omits %s: %v", code, promotionErr)
		}
	}
}

func TestTodo_WF_DISC_004_Property(t *testing.T) {
	priv, _ := testKeys(t)
	cases := []struct {
		name   string
		mutate func(*PromotionClosure)
		code   string
	}{
		{"missing exploratory digest", func(c *PromotionClosure) { c.ExploratoryDigest = "nope" }, "MISSING_EXPLORATORY_DIGEST"},
		{"missing graph digest", func(c *PromotionClosure) { c.GraphDigest = "" }, "MISSING_GRAPH_DIGEST"},
		{"unresolved schema", func(c *PromotionClosure) { c.Schemas[0].Digest = "sha256:xyz" }, "UNRESOLVED_SCHEMA"},
		{"floating capability", func(c *PromotionClosure) { c.Capabilities["identity-resolve"] = "*" }, "UNVERSIONED_CAPABILITY"},
		{"untyped behavior", func(c *PromotionClosure) { c.Behaviors[0].Type = "" }, "UNTYPED_BEHAVIOR"},
		{"reasonless deviation", func(c *PromotionClosure) {
			c.LegacyTouched = true
			c.Deviations = []Deviation{{Description: "shortcut", Reason: ""}}
		}, "UNSAFE_LEGACY_SHORTCUT"},
		{"legacy without record", func(c *PromotionClosure) { c.LegacyTouched = true }, "MISSING_PARITY_RECORD"},
		{"missing owner", func(c *PromotionClosure) { c.Owner = "  " }, "MISSING_OWNER"},
		{"bad expiry", func(c *PromotionClosure) { c.ReviewExpiry = "soon" }, "INVALID_EXPIRY"},
		{"expired review", func(c *PromotionClosure) { c.ReviewExpiry = "2000-01-01" }, "EXPIRED_REVIEW"},
		{"no fixtures", func(c *PromotionClosure) { c.Fixtures = nil }, "MISSING_NEGATIVE_FIXTURES"},
		{"unproven fixture", func(c *PromotionClosure) {
			c.Fixtures[0].Expect = workflowdecisions.DefaultRouteHuman
		}, "UNSAFE_FIXTURE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			closure := completeClosure()
			tc.mutate(&closure)
			receipt, err := Promote(closure, priv, "2098-06-01")
			if err == nil {
				t.Fatalf("mutation %s promoted", tc.name)
			}
			if !strings.Contains(err.Error(), tc.code) {
				t.Errorf("mutation %s missed %s: %v", tc.name, tc.code, err)
			}
			if receipt.Signature != "" {
				t.Errorf("mutation %s published a signature", tc.name)
			}
		})
	}
}

func TestTodo_WF_DISC_004_Golden(t *testing.T) {
	priv, _ := testKeys(t)
	receipt, err := Promote(completeClosure(), priv, "2098-06-01")
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "fixture", "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != strings.TrimSpace(string(want)) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestTodo_WF_DISC_004_Conformance(t *testing.T) {
	priv, pub := testKeys(t)
	closure := completeClosure()
	closure.ExploratoryDigest = liveRegistryDigest(t)
	closure.GraphDigest = liveCatalogDigest(t)
	receipt, err := Promote(closure, priv, "2098-06-01")
	if err != nil {
		t.Fatalf("live-digest closure rejected: %v", err)
	}
	ok, reason := VerifyReceipt(receipt, pub, "2098-06-01")
	if !ok {
		t.Fatalf("live-digest receipt does not verify: %s", reason)
	}
	if receipt.ExploratoryDigest != closure.ExploratoryDigest || receipt.GraphDigest != closure.GraphDigest {
		t.Fatal("receipt does not bind the live corpus digests")
	}
}

func TestTodo_WF_DISC_004_Mutation(t *testing.T) {
	priv, pub := testKeys(t)
	receipt, err := Promote(completeClosure(), priv, "2098-06-01")
	if err != nil {
		t.Fatal(err)
	}
	tampered := receipt
	tampered.Owner = "someone-else"
	if ok, _ := VerifyReceipt(tampered, pub, "2098-06-01"); ok {
		t.Fatal("tampered receipt verifies")
	}
	otherPub := "ab" + pub[2:]
	if ok, _ := VerifyReceipt(receipt, otherPub, "2098-06-01"); ok {
		t.Fatal("receipt verifies under the wrong key")
	}
	if ok, reason := VerifyReceipt(receipt, pub, "2099-06-01"); ok {
		t.Fatal("expired receipt verifies")
	} else if !strings.Contains(reason, "expired") {
		t.Fatalf("expiry refusal unexplained: %s", reason)
	}
	resigned, err := Promote(completeClosure(), priv, "2098-06-01")
	if err != nil {
		t.Fatal(err)
	}
	if resigned.Signature != receipt.Signature || resigned.Digest != receipt.Digest {
		t.Fatal("identical promotion did not reproduce the identical receipt")
	}
}

func liveRegistryDigest(t *testing.T) string {
	t.Helper()
	registry, err := workflowregistry.Scan(filepath.Join("..", "..", "..", "planning", "workflows"))
	if err != nil {
		t.Fatal(err)
	}
	return registry.Digest
}

func liveCatalogDigest(t *testing.T) string {
	t.Helper()
	report, err := workflowarchetypes.ScanCatalogs(
		filepath.Join("..", "..", "..", "planning", "workflows"),
		[]string{
			"people/catalog.md",
			"workforce/catalog.md",
			"rewards/catalog.md",
			"lifecycle/catalog.md",
			"leave/catalog.md",
			"hr-service/catalog.md",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return report.Digest
}
