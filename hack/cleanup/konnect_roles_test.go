package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/Kong/sdk-konnect-go/retry"
	"github.com/go-logr/logr"
)

// fakeKonnectUserRoles serves the /v3/users/{userID}/assigned-roles endpoint
// with the given pages. A page with non-200 status simulates a failing page.
func fakeKonnectUserRoles(t *testing.T, pages map[int]fakePage) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v3/users/test-user/assigned-roles") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		pageNumber := 0
		if _, err := fmt.Sscanf(r.URL.Query().Get("page[number]"), "%d", &pageNumber); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		page, ok := pages[pageNumber]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if page.status != http.StatusOK {
			w.WriteHeader(page.status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		roles := make([]string, 0, len(page.roleIDs))
		for _, id := range page.roleIDs {
			roles = append(roles, fmt.Sprintf(`{"id":%q,"entity_id":%q,"entity_type_name":%q}`,
				id, id, "AI Gateways"))
		}
		fmt.Fprintf(w, `{"meta":{"page":{"number":%d,"size":100,"total":%d}},"data":[%s]}`,
			pageNumber, page.total, strings.Join(roles, ","))
	}))
	t.Cleanup(srv.Close)
	return srv
}

type fakePage struct {
	status  int
	total   int
	roleIDs []string
}

// shortenRetryDelays makes the retry backoff instant for the duration of the test.
func shortenRetryDelays(t *testing.T) {
	t.Helper()
	userRolesRetryBaseDelay = time.Millisecond
	t.Cleanup(func() { userRolesRetryBaseDelay = time.Second })
}

func TestListUserRolesPagedPartialPageFailure(t *testing.T) {
	shortenRetryDelays(t)

	srv := fakeKonnectUserRoles(t, map[int]fakePage{
		1: {status: http.StatusOK, total: 300, roleIDs: ids("p1", 100)},
		2: {status: http.StatusInternalServerError},
		3: {status: http.StatusOK, total: 300, roleIDs: ids("p3", 100)},
	})

	roles, err := listUserRolesPaged(t.Context(), logr.Discard(), srv.URL, "token", "test-user")
	if err == nil {
		t.Error("expected an error for the failed page 2, got nil")
	}
	if len(roles) != 200 {
		t.Errorf("expected 200 roles from the successfully fetched pages, got %d", len(roles))
	}
}

func TestListUserRolesPagedAllPagesSuccess(t *testing.T) {
	shortenRetryDelays(t)

	srv := fakeKonnectUserRoles(t, map[int]fakePage{
		1: {status: http.StatusOK, total: 300, roleIDs: ids("p1", 100)},
		2: {status: http.StatusOK, total: 300, roleIDs: ids("p2", 100)},
		3: {status: http.StatusOK, total: 300, roleIDs: ids("p3", 100)},
	})

	roles, err := listUserRolesPaged(t.Context(), logr.Discard(), srv.URL, "token", "test-user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(roles) != 300 {
		t.Errorf("expected 300 roles, got %d", len(roles))
	}
	// Every role ID must be present exactly once - no page may be skipped.
	seen := make(map[string]struct{})
	for _, role := range roles {
		if _, dup := seen[*role.ID]; dup {
			t.Errorf("duplicate role %s", *role.ID)
		}
		seen[*role.ID] = struct{}{}
	}
}

// TestRemoveRolesNotFoundSkippedAndCounted verifies that removeRoles skips
// roles that no longer exist (404) and counts only the removed ones.
func TestRemoveRolesNotFoundSkippedAndCounted(t *testing.T) {
	var (
		mu             sync.Mutex
		removedRoleIDs = make(map[string]struct{})
	)
	notFound := func(w http.ResponseWriter) {
		// The SDK only deserializes the 404 into sdkerrors.NotFoundError when
		// the response has the application/problem+json content type.
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"status":404,"title":"Not Found","type":"https://kongapi.info/konnect/not-found","detail":"The resource requested was not found."}`)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "/v3/users/test-user/assigned-roles/"
		if r.Method != http.MethodDelete || !strings.HasPrefix(r.URL.Path, prefix) {
			notFound(w)
			return
		}
		roleID := strings.TrimPrefix(r.URL.Path, prefix)
		if strings.HasSuffix(roleID, "-gone") {
			notFound(w)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		removedRoleIDs[roleID] = struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	sdk := testSDK(srv.URL)

	removed, err := removeRoles(t.Context(), logr.Discard(), sdk.Roles, "test-user", []string{
		"role-1", "role-2-gone", "role-3",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed != 2 {
		t.Errorf("expected 2 removed roles, got %d", removed)
	}
	if _, ok := removedRoleIDs["role-2-gone"]; ok {
		t.Error("role-2-gone should have been skipped (404), but was removed")
	}
	if _, ok := removedRoleIDs["role-1"]; !ok {
		t.Error("role-1 was not removed")
	}
	if _, ok := removedRoleIDs["role-3"]; !ok {
		t.Error("role-3 was not removed")
	}
}

func TestRemoveRolesAllFailuresJoined(t *testing.T) {
	shortenRetryDelays(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	sdk := testSDK(srv.URL)

	_, err := removeRoles(t.Context(), logr.Discard(), sdk.Roles, "test-user", []string{"role-1", "role-2"})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "role-1") || !strings.Contains(err.Error(), "role-2") {
		t.Errorf("expected both role failures to be joined, got: %v", err)
	}
}

// testSDK returns an SDK whose HTTP client redirects all requests to the given
// server URL. The SDK's operations use a hardcoded global.api.konghq.com server
// list, so the client-level redirect is the only way to point them at a test
// server.
func testSDK(serverURL string) *sdkkonnectgo.SDK {
	target, err := url.Parse(serverURL)
	if err != nil {
		panic(err)
	}
	client := &http.Client{
		Transport: redirectTransport{target: target},
	}
	return sdkkonnectgo.New(
		sdkkonnectgo.WithSecurity(sdkkonnectcomp.Security{PersonalAccessToken: new("token")}),
		sdkkonnectgo.WithClient(client),
		// Disable the SDK-level retries so that the tests don't take long.
		sdkkonnectgo.WithRetryConfig(retry.Config{Strategy: "none"}),
	)
}

type redirectTransport struct {
	target *url.URL
}

func (t redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	u := *req.URL
	u.Scheme = t.target.Scheme
	u.Host = t.target.Host
	req.URL = &u
	return http.DefaultTransport.RoundTrip(req)
}

func ids(prefix string, n int) []string { //nolint:unparam
	result := make([]string, 0, n)
	for i := range n {
		result = append(result, fmt.Sprintf("%s-%03d", prefix, i))
	}
	return result
}
