// Package workspace_test is the conformance suite for the served Promotion
// workspace (internal/humanwork/workspace).
//
// It exists because what the workspace claims is not true of any one package:
// it is true of a composed cell serving HTML. The suite migrates an ephemeral
// PostgreSQL from zero, composes the same cell cmd/hcmnext serves
// (internal/intent/app.NewCell), drives the workspace over its real HTTP edge
// with real credentials, and then reads the database directly to check that
// looking at the page wrote nothing.
package workspace_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	transportcell "github.com/monstercameron/human-capital-management-suite/internal/transport/cell"
	transportedge "github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// Fixture identity. The tenant is the corpus tenant, because the workspace
// reads the design-partner corpus and a suite that invented a second
// population would be testing a page nobody serves.
const (
	testTenant   = string(fixtures.Tenant)
	testIssuer   = "https://issuer.test.hcm-next.invalid"
	testAudience = "hcm-next-api"
	testOrgScope = "org-north-america"
	testCellID   = "cell-workspace-test"

	// testWorker is the corpus's own promotion scenario worker.
	testWorker = "omar-reyes"

	// authorizedPurpose is the purpose the compensation half of the workspace
	// is granted under. operationsPurpose is authorized for the same
	// principal but carries no compensation grant, which is what makes the
	// masked-field case a real policy decision rather than a flag.
	authorizedPurpose = authz.PurposeCompensationReview
	operationsPurpose = "hcm_operations"
)

// testSigningKey never leaves this package and authenticates nothing outside
// a test process.
var testSigningKey = []byte("hcm-next-workspace-suite-signing-key-32+")

// baseTime pins the credential window and the cell clock.
var baseTime = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// principal describes one caller the suite drives the workspace as.
type principal struct {
	name     string
	subject  string
	roles    []string
	purposes []string
}

var (
	// compAdmin may see the whole promotion, pay included.
	compAdmin = principal{
		name:     "comp-admin",
		subject:  "user-0191f3c4",
		roles:    []string{"intent_author", string(authz.RoleCompAdmin)},
		purposes: []string{authorizedPurpose},
	}
	// operations reaches the worker but not their compensation: same roles,
	// a purpose with no grant over the compensation domain.
	operations = principal{
		name:     "operations",
		subject:  "user-0191f3c5",
		roles:    []string{"intent_author", string(authz.RoleCompAdmin)},
		purposes: []string{operationsPurpose},
	}
	// unrelated holds no role that reaches this subject at all.
	unrelated = principal{
		name:     "unrelated",
		subject:  "user-0191f3c6",
		roles:    []string{"intent_author"},
		purposes: []string{authorizedPurpose},
	}
)

// cell is one composed, running P1A cell serving its real HTTP edge, plus
// direct database access so an assertion can compare what a page showed
// against what the cell wrote.
type cell struct {
	t *testing.T

	pool *pgxadapter.Pool
	app  *app.Cell

	url    string
	client *http.Client
	tokens map[string]string
	// browserCookie is the edge-issued SameSite proof a real browser stores
	// after its first workspace GET. Keeping it here makes POST helpers drive
	// the complete browser boundary rather than bypassing it.
	browserCookie *http.Cookie
}

// newCell migrates a private schema, composes the cell and serves its edge.
//
// The composition call is internal/intent/app.NewCell and the handler is
// Cell.EdgeHandler, which is exactly what cmd/hcmnext runs. A suite that
// mounted the workspace handler directly would prove the handler works and
// nothing about whether the binary serves it.
//
// devBrowserLogin is variadic and defaults to false so every existing call
// site - which only ever cared about workspaceEnabled - keeps compiling
// unchanged; a test that wants the dev sign-in flow on passes a second true.
func newCell(t *testing.T, workspaceEnabled bool, devBrowserLogin ...bool) *cell {
	t.Helper()
	loginEnabled := len(devBrowserLogin) > 0 && devBrowserLogin[0]

	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	store, err := pgstore.New(pool, pgstore.WithCellID(testCellID),
		pgstore.WithClock(func() time.Time { return baseTime }))
	if err != nil {
		t.Fatalf("pgstore.New: %v", err)
	}
	if err := store.Bootstrap(context.Background(), testTenant); err != nil {
		t.Fatalf("bootstrap tenant: %v", err)
	}

	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      testSigningKey,
		Issuer:   testIssuer,
		Audience: testAudience,
		Now:      func() time.Time { return baseTime },
	})
	if err != nil {
		t.Fatalf("NewHMACVerifier: %v", err)
	}

	composed, err := app.NewCell(app.CellConfig{
		Store:           store,
		Verifier:        verifier,
		Audience:        testAudience,
		MaxDeadline:     30 * time.Second,
		Now:             func() time.Time { return baseTime },
		Workspace:       &workspaceEnabled,
		DevBrowserLogin: loginEnabled,
	})
	if err != nil {
		t.Fatalf("app.NewCell: %v", err)
	}
	handler, err := transportcell.NewEdgeHandler(composed)
	if err != nil {
		t.Fatalf("EdgeHandler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	c := &cell{
		t:      t,
		pool:   pool,
		app:    composed,
		url:    server.URL,
		client: server.Client(),
		tokens: map[string]string{},
	}
	// Redirects are not followed: a route that answered 3xx instead of the
	// page would otherwise pass an assertion about the page it redirected to.
	c.client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	for _, p := range []principal{compAdmin, operations, unrelated} {
		c.tokens[p.name] = issue(t, verifier, p)
	}
	return c
}

// issue mints one bearer credential.
func issue(t *testing.T, verifier *trust.HMACVerifier, p principal) string {
	t.Helper()
	token, err := verifier.Issue(trust.Claims{
		Issuer:               testIssuer,
		Audience:             testAudience,
		Subject:              p.subject,
		SubjectKind:          "human",
		Tenant:               testTenant,
		OrganizationScopeID:  testOrgScope,
		Roles:                p.roles,
		AuthorityRefs:        []string{"authority:position:vp-people"},
		Purposes:             p.purposes,
		AuthenticationMethod: "bearer_token",
		Assurance:            "substantial",
		SessionRef:           "session-workspace-" + p.name,
		IssuedAtUnix:         baseTime.Add(-time.Minute).Unix(),
		ExpiresAtUnix:        baseTime.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue credential for %s: %v", p.name, err)
	}
	return "Bearer " + token
}

// response is one workspace answer, read to completion.
type response struct {
	Status  int
	Header  http.Header
	Body    string
	Cookies []*http.Cookie
}

// get issues one GET. An empty principal name sends no credential at all.
func (c *cell) get(path, as string, cookies ...*http.Cookie) response {
	c.t.Helper()
	req, err := http.NewRequest(http.MethodGet, c.url+path, nil)
	if err != nil {
		c.t.Fatalf("build GET %s: %v", path, err)
	}
	return c.do(req, as, cookies)
}

// post submits one form.
func (c *cell) post(path, as string, form map[string]string, cookies ...*http.Cookie) response {
	c.t.Helper()
	values := url.Values{}
	for k, v := range form {
		values.Set(k, v)
	}
	req, err := http.NewRequest(http.MethodPost, c.url+path, strings.NewReader(values.Encode()))
	if err != nil {
		c.t.Fatalf("build POST %s: %v", path, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if c.browserCookie != nil {
		req.Header.Set("Origin", c.url)
		cookies = append(cookies, c.browserCookie)
	}
	return c.do(req, as, cookies)
}

func (c *cell) do(req *http.Request, as string, cookies []*http.Cookie) response {
	c.t.Helper()
	if as != "" {
		token, ok := c.tokens[as]
		if !ok {
			c.t.Fatalf("no credential for principal %q", as)
		}
		req.Header.Set("authorization", token)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	res, err := c.client.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		c.t.Fatalf("read %s %s: %v", req.Method, req.URL.Path, err)
	}
	responseCookies := res.Cookies()
	for _, cookie := range responseCookies {
		if cookie.Name == transportedge.BrowserCSRFCookieName {
			copy := *cookie
			c.browserCookie = &copy
		}
	}
	return response{Status: res.StatusCode, Header: res.Header, Body: string(body), Cookies: responseCookies}
}

// ---------------------------------------------------------------------------
// Reading the page back
// ---------------------------------------------------------------------------

var csrfRE = regexp.MustCompile(`name="` + workspace.ParamCSRF + `" value="([^"]*)"`)

// csrfToken pulls the token the rendered page embedded.
func (r response) csrfToken(t *testing.T) string {
	t.Helper()
	m := csrfRE.FindStringSubmatch(r.Body)
	if m == nil {
		t.Fatal("the rendered workspace carries no CSRF token")
	}
	return m[1]
}

// sessionCookie returns the session cookie the page set.
func (r response) sessionCookie(t *testing.T) *http.Cookie {
	t.Helper()
	for _, c := range r.Cookies {
		if c.Name == "hcmnext_workspace_session" {
			return c
		}
	}
	t.Fatal("the rendered workspace set no session cookie")
	return nil
}

// ---------------------------------------------------------------------------
// Database fingerprint
// ---------------------------------------------------------------------------

// fingerprint is the row count of every table in this cell's own schema.
//
// It covers the chronology tables too, which is stricter than the workspace's
// stated contract: the workspace never creates an intent, so it appends no
// ledger event either, and there is no table it is allowed to have touched.
// Counting everything means a write the workspace was not supposed to make
// cannot hide in a table nobody thought to list.
func (c *cell) fingerprint() string {
	c.t.Helper()
	ctx := context.Background()
	rows, err := c.pool.Query(ctx, `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'
		ORDER BY table_name`)
	if err != nil {
		c.t.Fatalf("list tables: %v", err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			c.t.Fatalf("scan table name: %v", err)
		}
		names = append(names, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		c.t.Fatalf("list tables: %v", err)
	}
	if len(names) == 0 {
		c.t.Fatal("the migrated schema declares no table; the fingerprint would be vacuous")
	}

	var out strings.Builder
	for _, name := range names {
		var count int64
		if err := c.pool.QueryRow(ctx, `SELECT count(*) FROM "`+name+`"`).Scan(&count); err != nil {
			c.t.Fatalf("count %s: %v", name, err)
		}
		fmt.Fprintf(&out, "%s=%d;", name, count)
	}
	return out.String()
}
