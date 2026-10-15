// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testKey = "test-api-key-0123456789"

// Fixtures follow upstream responses (app/api/serializer.py serialize_alias_info_v2).
const aliasFixture = `{
  "creation_date": "2020-04-06 17:57:14+00:00",
  "creation_timestamp": 1586195834,
  "email": "admin@example.com",
  "name": null,
  "enabled": true,
  "id": 3,
  "mailbox": {"email": "a@b.c", "id": 1},
  "mailboxes": [{"email": "m1@cd.ef", "id": 2}, {"email": "john@wick.com", "id": 1}],
  "latest_activity": null,
  "nb_block": 0, "nb_forward": 1, "nb_reply": 0,
  "note": null,
  "pinned": false,
  "support_pgp": false,
  "disable_pgp": false
}`

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, testKey, WithBackoff(func(int) time.Duration { return time.Millisecond }))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func writeJSON(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = io.WriteString(w, body)
}

func TestNew(t *testing.T) {
	if _, err := New("", ""); err == nil {
		t.Error("expected error for empty API key")
	}
	for _, u := range []string{"ftp://x", "not a url", "/relative"} {
		if _, err := New(u, "k"); err == nil {
			t.Errorf("expected error for base URL %q", u)
		}
	}
	c, err := New("", "k")
	if err != nil {
		t.Fatal(err)
	}
	if c.baseURL.String() != DefaultBaseURL {
		t.Errorf("default base URL = %s", c.baseURL)
	}
}

func TestHeaders(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authentication"); got != testKey {
			t.Errorf("Authentication header = %q", got)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("unexpected Authorization header")
		}
		if r.Method == http.MethodPatch && r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		writeJSON(w, 200, aliasFixture)
	})
	if _, err := c.GetAlias(context.Background(), 3, ""); err != nil {
		t.Fatal(err)
	}
	note := "x"
	if err := c.UpdateAlias(context.Background(), 3, AliasUpdate{Note: &note}); err != nil {
		t.Fatal(err)
	}
}

func TestBaseURLWithPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSON(w, 200, `{"mailboxes": []}`)
	}))
	defer srv.Close()
	c, _ := New(srv.URL+"/sl/", testKey)
	if _, err := c.ListMailboxes(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/sl/api/v2/mailboxes" {
		t.Errorf("path = %s", gotPath)
	}
}

func TestUnauthorized(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 401, `{"error": "Wrong api key"}`)
	})
	_, err := c.ListMailboxes(context.Background())
	if !IsUnauthorized(err) {
		t.Fatalf("expected 401, got %v", err)
	}
	if !strings.Contains(err.Error(), "Wrong api key") || !strings.Contains(err.Error(), "SIMPLELOGIN_API_KEY") {
		t.Errorf("unhelpful error: %v", err)
	}
	if strings.Contains(err.Error(), testKey) {
		t.Error("error leaks the API key")
	}
}

func TestErrorBodies(t *testing.T) {
	tests := []struct {
		name, body string
		code       int
		want       string
		notWant    string
	}{
		{"json", `{"error": "Errors with Mailbox"}`, 400, "HTTP 400: Errors with Mailbox", ""},
		{"html", `<html>secret-ish proxy page</html>`, 502, "HTTP 502", "proxy page"},
		{"long", `{"error": "` + strings.Repeat("a", 2000) + `"}`, 400, "...", strings.Repeat("a", 600)},
		{"forbidden", `{"error": "Forbidden"}`, 403, "Forbidden", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, tt.code, tt.body) })
			c.maxRetries = 0
			err := c.DeleteAlias(context.Background(), 1)
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != tt.code {
				t.Fatalf("got %v", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("%q does not contain %q", err, tt.want)
			}
			if tt.notWant != "" && strings.Contains(err.Error(), tt.notWant) {
				t.Errorf("%q contains %q", err, tt.notWant)
			}
		})
	}
}

func TestGetRetriesTransientErrors(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.Header().Set("Retry-After", "0")
			writeJSON(w, 429, `{"error": "rate limited"}`)
		case 2:
			writeJSON(w, 503, `{}`)
		default:
			writeJSON(w, 200, `{"mailboxes": [{"id": 1, "email": "a@b.c", "verified": true, "default": true}]}`)
		}
	})
	mbs, err := c.ListMailboxes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(mbs) != 1 || calls.Load() != 3 {
		t.Errorf("mailboxes=%v calls=%d", mbs, calls.Load())
	}
}

func TestGetRetriesAreBounded(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeJSON(w, 503, `{}`)
	})
	if _, err := c.ListMailboxes(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if calls.Load() != int32(defaultMaxRetries+1) {
		t.Errorf("calls = %d", calls.Load())
	}
}

func TestLongRetryAfterIsNotWaited(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "3600")
		writeJSON(w, 429, `{"error": "too many"}`)
	})
	_, err := c.ListMailboxes(context.Background())
	if !IsRateLimited(err) || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func TestMutationsAreNeverRetried(t *testing.T) {
	for _, code := range []int{429, 500, 502, 503} {
		var calls atomic.Int32
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.Header().Set("Retry-After", "0")
			writeJSON(w, code, `{}`)
		})
		ctx := context.Background()
		_, _ = c.ToggleAlias(ctx, 1)
		_, _ = c.CreateCustomAlias(ctx, CreateAliasRequest{AliasPrefix: "a", SignedSuffix: "s", MailboxIDs: []int64{1}})
		_ = c.DeleteAlias(ctx, 1)
		n := "x"
		_ = c.UpdateAlias(ctx, 1, AliasUpdate{Note: &n})
		if calls.Load() != 4 {
			t.Errorf("status %d: %d calls for 4 mutations", code, calls.Load())
		}
	}
}

func TestContextCancellation(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.ListMailboxes(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("cancellation was not prompt")
	}
}

func TestCancellationDuringBackoff(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 503, `{}`) })
	c.backoff = func(int) time.Duration { return 30 * time.Second }
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.ListMailboxes(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}

func TestGetAlias(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/aliases/3" || r.Method != http.MethodGet {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, 200, aliasFixture)
	})
	a, err := c.GetAlias(context.Background(), 3, "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Email != "admin@example.com" || !a.Enabled || a.Note != nil || a.Name != nil {
		t.Errorf("unexpected alias %+v", a)
	}
	if got := a.MailboxIDs(); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("MailboxIDs = %v, want sorted [1 2]", got)
	}
}

func TestMailboxIDsDeduplicates(t *testing.T) {
	a := Alias{Mailboxes: []MailboxRef{{ID: 5}, {ID: 1}, {ID: 5}}}
	if got := a.MailboxIDs(); len(got) != 2 || got[0] != 1 || got[1] != 5 {
		t.Errorf("MailboxIDs = %v", got)
	}
}

func TestGetAliasMissing(t *testing.T) {
	// listed reports whether the search listing still contains the alias.
	type tc struct {
		name       string
		status     int
		knownEmail string
		listed     bool
		listStatus int
		wantGone   bool
	}
	tests := []tc{
		{name: "404", status: 404, wantGone: true},
		{name: "400 confirmed by listing", status: 400, knownEmail: "admin@example.com", wantGone: true},
		{name: "400 but still listed", status: 400, knownEmail: "admin@example.com", listed: true},
		{name: "400 listing fails", status: 400, knownEmail: "admin@example.com", listStatus: 500},
		{name: "400 without known email", status: 400},
		{name: "403 is not deletion", status: 403, knownEmail: "admin@example.com"},
		{name: "401 is not deletion", status: 401, knownEmail: "admin@example.com"},
		{name: "500 is not deletion", status: 500, knownEmail: "admin@example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/aliases/3":
					writeJSON(w, tt.status, `{"error": "Unknown error"}`)
				case "/api/v2/aliases":
					if tt.listStatus != 0 {
						writeJSON(w, tt.listStatus, `{}`)
						return
					}
					var body struct{ Query string }
					_ = json.NewDecoder(r.Body).Decode(&body)
					if body.Query != tt.knownEmail || r.URL.Query().Get("page_id") == "" {
						t.Errorf("bad search: query=%q page_id=%q", body.Query, r.URL.Query().Get("page_id"))
					}
					if tt.listed && r.URL.Query().Get("page_id") == "0" {
						writeJSON(w, 200, `{"aliases": [`+aliasFixture+`]}`)
						return
					}
					// Another alias matching the search, then an empty page.
					if r.URL.Query().Get("page_id") == "0" {
						writeJSON(w, 200, `{"aliases": [{"id": 99, "email": "admin@example.com.other.test", "mailboxes": []}]}`)
						return
					}
					writeJSON(w, 200, `{"aliases": []}`)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
				}
			})
			c.maxRetries = 0
			_, err := c.GetAlias(context.Background(), 3, tt.knownEmail)
			if err == nil {
				t.Fatal("expected error")
			}
			if gone := errors.Is(err, ErrNotFound); gone != tt.wantGone {
				t.Errorf("ErrNotFound=%v, want %v (err: %v)", gone, tt.wantGone, err)
			}
		})
	}
}

func TestCreateCustomAlias(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v3/alias/custom/new" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["alias_prefix"] != "admin" || body["signed_suffix"] != "@example.com.X6_7OQ.sig" {
			t.Errorf("body = %v", body)
		}
		if ids, _ := body["mailbox_ids"].([]any); len(ids) != 2 {
			t.Errorf("mailbox_ids = %v", body["mailbox_ids"])
		}
		if _, ok := body["name"]; ok {
			t.Error("name should be omitted when nil")
		}
		if body["note"] != "hi" {
			t.Errorf("note = %v", body["note"])
		}
		writeJSON(w, 201, `{"alias": "admin@example.com", `+strings.TrimPrefix(aliasFixture, "{"))
	})
	note := "hi"
	a, err := c.CreateCustomAlias(context.Background(), CreateAliasRequest{
		AliasPrefix: "admin", SignedSuffix: "@example.com.X6_7OQ.sig", MailboxIDs: []int64{1, 2}, Note: &note,
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != 3 || a.Email != "admin@example.com" {
		t.Errorf("alias = %+v", a)
	}
}

func TestCreateConflict(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 409, `{"error": "alias admin@example.com already exists"}`)
	})
	_, err := c.CreateCustomAlias(context.Background(), CreateAliasRequest{})
	if !IsConflict(err) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestUpdateAliasPayload(t *testing.T) {
	note, name := "new note", "Admin"
	tests := []struct {
		name string
		upd  AliasUpdate
		want string // "" means no request
	}{
		{"noop", AliasUpdate{}, ""},
		{"set", AliasUpdate{Note: &note, Name: &name, MailboxIDs: []int64{1, 2}}, `{"mailbox_ids":[1,2],"name":"Admin","note":"new note"}`},
		{"clear", AliasUpdate{ClearNote: true, ClearName: true}, `{"name":null,"note":null}`},
		{"mailboxes only", AliasUpdate{MailboxIDs: []int64{7}}, `{"mailbox_ids":[7]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPatch || r.URL.Path != "/api/aliases/3" {
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				}
				b, _ := io.ReadAll(r.Body)
				got = string(b)
				writeJSON(w, 200, `{"ok": true}`)
			})
			if err := c.UpdateAlias(context.Background(), 3, tt.upd); err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("body = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestToggleAlias(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/aliases/3/toggle" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, 200, `{"enabled": false}`)
	})
	enabled, err := c.ToggleAlias(context.Background(), 3)
	if err != nil || enabled {
		t.Fatalf("enabled=%v err=%v", enabled, err)
	}

	c = newTestClient(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, `{}`) })
	if _, err := c.ToggleAlias(context.Background(), 3); err == nil {
		t.Error("expected error when response lacks enabled")
	}
}

func TestDeleteAlias(t *testing.T) {
	var called bool
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = r.Method == http.MethodDelete && r.URL.Path == "/api/aliases/3"
		writeJSON(w, 200, `{"deleted": true}`)
	})
	if err := c.DeleteAlias(context.Background(), 3); err != nil || !called {
		t.Fatalf("called=%v err=%v", called, err)
	}
}

func TestSplitAliasEmail(t *testing.T) {
	good := map[string][2]string{
		"admin@example.com":              {"admin", "example.com"},
		"a.b-c_d@mail.example.org":       {"a.b-c_d", "mail.example.org"},
		strings.Repeat("a", 40) + "@x.y": {strings.Repeat("a", 40), "x.y"},
	}
	for email, want := range good {
		p, d, err := SplitAliasEmail(email)
		if err != nil || p != want[0] || d != want[1] {
			t.Errorf("SplitAliasEmail(%q) = %q, %q, %v", email, p, d, err)
		}
	}
	bad := []string{
		"", "admin", "@example.com", "admin@", "Admin@example.com", "admin@Example.com",
		"a+b@example.com", "a b@example.com", ".a@example.com", "a.@example.com", "a..b@example.com",
		strings.Repeat("a", 41) + "@x.y", "a@b@c", "jöhn@example.com",
	}
	for _, email := range bad {
		if _, _, err := SplitAliasEmail(email); err == nil {
			t.Errorf("SplitAliasEmail(%q) accepted", email)
		}
	}
}

func TestFindExactSuffix(t *testing.T) {
	// Shape from GET /api/v5/alias/options for an account with a custom
	// domain that has random_prefix_generation enabled.
	var opts AliasOptions
	err := json.Unmarshal([]byte(`{
	  "can_create": true,
	  "prefix_suggestion": "",
	  "suffixes": [
	    {"signed_suffix": ".cat@example.com.X6_7OQ.0e9N", "suffix": ".cat@example.com", "is_custom": true, "is_premium": false},
	    {"signed_suffix": "@example.com.X6_7OQ.TTgC", "suffix": "@example.com", "is_custom": true, "is_premium": false},
	    {"signed_suffix": "@sub.example.com.X6_7OQ.abcd", "suffix": "@sub.example.com", "is_custom": true, "is_premium": false},
	    {"signed_suffix": ".yeah@simplelogin.com.X6_7OQ.i8XL", "suffix": ".yeah@simplelogin.com", "is_custom": false, "is_premium": false}
	  ]}`), &opts)
	if err != nil {
		t.Fatal(err)
	}
	s, err := FindExactSuffix(&opts, "example.com")
	if err != nil || s.SignedSuffix != "@example.com.X6_7OQ.TTgC" {
		t.Errorf("got %+v, %v", s, err)
	}
	if s, err := FindExactSuffix(&opts, "sub.example.com"); err != nil || s.Suffix != "@sub.example.com" {
		t.Errorf("got %+v, %v", s, err)
	}
	for _, d := range []string{"simplelogin.com", "other.com", "ample.com"} {
		_, err := FindExactSuffix(&opts, d)
		if err == nil {
			t.Errorf("domain %s accepted", d)
			continue
		}
		if !strings.Contains(err.Error(), "example.com, sub.example.com") {
			t.Errorf("error does not list usable domains: %v", err)
		}
		if strings.Contains(err.Error(), "X6_7OQ") {
			t.Errorf("error leaks signed suffix: %v", err)
		}
	}
}

func TestListCustomDomains(t *testing.T) {
	const domain = `{"catch_all": false, "creation_date": "2021-03-10 21:36:08+00:00", "creation_timestamp": 1615412168,
	  "domain_name": "Example.com", "id": 1, "is_verified": true, "mailboxes": [{"email": "a@b.c", "id": 1}],
	  "name": null, "nb_alias": 0, "random_prefix_generation": false}`
	for name, body := range map[string]string{
		"upstream object":  `{"custom_domains": [` + domain + `]}`,
		"documented array": `[` + domain + `]`,
	} {
		t.Run(name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, body) })
			d, err := c.FindCustomDomain(context.Background(), "example.com")
			if err != nil {
				t.Fatal(err)
			}
			if d.ID != 1 || !d.IsVerified || d.Name != nil || len(d.Mailboxes) != 1 {
				t.Errorf("domain = %+v", d)
			}
			if _, err := c.FindCustomDomain(context.Background(), "missing.com"); !errors.Is(err, ErrNotFound) {
				t.Errorf("expected ErrNotFound, got %v", err)
			}
		})
	}
}

func TestFindMailboxByEmail(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/mailboxes" {
			t.Errorf("path = %s", r.URL.Path)
		}
		writeJSON(w, 200, `{"mailboxes": [
		  {"email": "a@b.c", "id": 1, "default": true, "creation_timestamp": 1590918512, "nb_alias": 10, "verified": true},
		  {"email": "M1@example.com", "id": 2, "default": false, "creation_timestamp": 1590918512, "nb_alias": 0, "verified": false}]}`)
	})
	m, err := c.FindMailboxByEmail(context.Background(), "m1@example.com")
	if err != nil || m.ID != 2 || m.Verified || m.Default {
		t.Fatalf("m=%+v err=%v", m, err)
	}
	if _, err := c.FindMailboxByEmail(context.Background(), "nope@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// The not-found confirmation search is read-only and therefore retried.
func TestAliasSearchIsRetried(t *testing.T) {
	var searches atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/aliases/3":
			writeJSON(w, 400, `{"error": "Unknown error"}`)
		case "/api/v2/aliases":
			if searches.Add(1) == 1 {
				w.Header().Set("Retry-After", "0")
				writeJSON(w, 429, `{"error": "rate limited"}`)
				return
			}
			writeJSON(w, 200, `{"aliases": []}`)
		}
	})
	if _, err := c.GetAlias(context.Background(), 3, "admin@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if searches.Load() != 2 {
		t.Errorf("searches = %d, want 2", searches.Load())
	}
}

// Addresses are unique, so finding the address on another alias settles it
// without reading further pages.
func TestAliasSearchStopsAtExactAddress(t *testing.T) {
	var pages []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/aliases/3":
			writeJSON(w, 400, `{"error": "Unknown error"}`)
		case "/api/v2/aliases":
			pages = append(pages, r.URL.Query().Get("page_id"))
			writeJSON(w, 200, `{"aliases": [
			  {"id": 7, "email": "admin@example.com.other.test", "mailboxes": []},
			  {"id": 8, "email": "admin@example.com", "mailboxes": []}]}`)
		}
	})
	if _, err := c.GetAlias(context.Background(), 3, "admin@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if len(pages) != 1 {
		t.Errorf("searched pages %v, want only page 0", pages)
	}
}
