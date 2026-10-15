// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const fakeAPIKey = "fake-key"

// fakeSimpleLogin is an in-memory SimpleLogin API that mimics the upstream
// behaviour relevant to the provider, including its quirks:
//   - GET of a missing alias returns 400 "Unknown error"
//   - DELETE of a missing alias returns 403 "Forbidden"
//   - deleted custom-domain aliases go to a trash that blocks re-creation (409)
//   - alias mailboxes are returned in random order
//   - the search listing excludes deleted aliases
type fakeSimpleLogin struct {
	t   *testing.T
	srv *httptest.Server

	mu        sync.Mutex
	nextID    int64
	aliases   map[int64]*fakeAlias
	trash     map[string]bool
	mailboxes []fakeMailbox
	domains   []string
	// failGets makes GET /api/aliases/:id return 500.
	failGets bool
	// failNextGets makes the next n GET /api/aliases/:id return 500.
	failNextGets int
	// failToggles makes POST /api/aliases/:id/toggle return 502.
	failToggles bool

	// Counters of mutating calls.
	creates, patches, toggles, deletes int
}

type fakeAlias struct {
	ID         int64
	Email      string
	Note, Name *string
	Enabled    bool
	MailboxIDs []int64
}

type fakeMailbox struct {
	ID       int64
	Email    string
	Verified bool
}

func newFakeSimpleLogin(t *testing.T) *fakeSimpleLogin {
	f := &fakeSimpleLogin{
		t:       t,
		nextID:  100,
		aliases: map[int64]*fakeAlias{},
		trash:   map[string]bool{},
		mailboxes: []fakeMailbox{
			{ID: 1, Email: "primary@example.net", Verified: true},
			{ID: 2, Email: "secondary@example.net", Verified: true},
			{ID: 3, Email: "unverified@example.net", Verified: false},
		},
		domains: []string{"example.com"},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSimpleLogin) providerConfig() string {
	return fmt.Sprintf(`
terraform {
  required_providers {
    simplelogin = {
      source = "registry.terraform.io/qnimbus/simplelogin"
    }
  }
}

provider "simplelogin" {
  base_url = %q
  api_key  = %q
}
`, f.srv.URL, fakeAPIKey)
}

// addAlias inserts an alias directly, as if it existed before.
func (f *fakeSimpleLogin) addAlias(email string, mailboxIDs ...int64) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.aliases[f.nextID] = &fakeAlias{ID: f.nextID, Email: email, Enabled: true, MailboxIDs: mailboxIDs}
	return f.nextID
}

func (f *fakeSimpleLogin) aliasByEmail(email string) *fakeAlias {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.aliases {
		if a.Email == email {
			c := *a
			return &c
		}
	}
	return nil
}

func (f *fakeSimpleLogin) mutations() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.creates + f.patches + f.toggles + f.deletes
}

func (f *fakeSimpleLogin) handle(w http.ResponseWriter, r *http.Request) {
	reply := func(code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	fail := func(code int, msg string) { reply(code, map[string]string{"error": msg}) }

	if r.Header.Get("Authentication") != fakeAPIKey {
		fail(401, "Wrong api key")
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	p := r.URL.Path
	switch {
	case r.Method == http.MethodGet && p == "/api/v5/alias/options":
		var suffixes []map[string]any
		for _, d := range f.domains {
			suffixes = append(suffixes,
				map[string]any{"suffix": ".rand@" + d, "signed_suffix": ".rand@" + d + ".SIG", "is_custom": true, "is_premium": false},
				map[string]any{"suffix": "@" + d, "signed_suffix": "@" + d + ".SIG", "is_custom": true, "is_premium": false})
		}
		suffixes = append(suffixes, map[string]any{"suffix": ".word@simplelogin.com", "signed_suffix": ".word@simplelogin.com.SIG", "is_custom": false, "is_premium": false})
		reply(200, map[string]any{"can_create": true, "prefix_suggestion": "", "suffixes": suffixes})

	case r.Method == http.MethodPost && p == "/api/v3/alias/custom/new":
		var body struct {
			AliasPrefix  string  `json:"alias_prefix"`
			SignedSuffix string  `json:"signed_suffix"`
			MailboxIDs   []int64 `json:"mailbox_ids"`
			Note, Name   *string
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			fail(400, "request body does not follow the required format")
			return
		}
		suffix, ok := strings.CutSuffix(body.SignedSuffix, ".SIG")
		if !ok {
			fail(400, "Tampered suffix")
			return
		}
		if len(body.MailboxIDs) == 0 {
			fail(400, "At least one mailbox must be selected")
			return
		}
		for _, id := range body.MailboxIDs {
			if !f.verifiedMailbox(id) {
				fail(400, "Errors with Mailbox")
				return
			}
		}
		email := strings.ToLower(body.AliasPrefix) + suffix
		if f.trash[email] || f.findLocked(email) != nil {
			fail(409, fmt.Sprintf("alias %s already exists", email))
			return
		}
		f.creates++
		f.nextID++
		a := &fakeAlias{ID: f.nextID, Email: email, Enabled: true, MailboxIDs: body.MailboxIDs, Note: body.Note, Name: body.Name}
		f.aliases[a.ID] = a
		out := f.serialize(a)
		out["alias"] = email
		reply(201, out)

	case p == "/api/v2/aliases" && (r.Method == http.MethodPost || r.Method == http.MethodGet):
		page, err := strconv.Atoi(r.URL.Query().Get("page_id"))
		if err != nil {
			fail(400, "page_id must be provided in request query")
			return
		}
		var body struct{ Query string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		res := []map[string]any{}
		if page == 0 {
			for _, a := range f.aliases {
				if strings.Contains(a.Email, body.Query) {
					res = append(res, f.serialize(a))
				}
			}
		}
		reply(200, map[string]any{"aliases": res})

	case r.Method == http.MethodGet && p == "/api/v2/mailboxes":
		var res []map[string]any
		for _, m := range f.mailboxes {
			res = append(res, map[string]any{"id": m.ID, "email": m.Email, "verified": m.Verified, "default": m.ID == 1, "creation_timestamp": 1590918512, "nb_alias": 0})
		}
		reply(200, map[string]any{"mailboxes": res})

	case r.Method == http.MethodGet && p == "/api/custom_domains":
		var res []map[string]any
		for i, d := range f.domains {
			res = append(res, map[string]any{
				"id": i + 1, "domain_name": d, "is_verified": true, "catch_all": false, "name": nil,
				"random_prefix_generation": true, "nb_alias": 0, "creation_timestamp": 1615412168,
				"creation_date": "2021-03-10 21:36:08+00:00", "mailboxes": []map[string]any{{"id": 1, "email": "primary@example.net"}},
			})
		}
		reply(200, map[string]any{"custom_domains": res})

	case strings.HasPrefix(p, "/api/aliases/"):
		rest := strings.TrimPrefix(p, "/api/aliases/")
		idStr, action, _ := strings.Cut(rest, "/")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			fail(404, "not found")
			return
		}
		a := f.aliases[id]
		switch {
		case r.Method == http.MethodGet && action == "":
			if f.failGets || f.failNextGets > 0 {
				f.failNextGets--
				fail(500, "Internal error")
				return
			}
			if a == nil {
				fail(400, "Unknown error")
				return
			}
			reply(200, f.serialize(a))
		case r.Method == http.MethodPatch && action == "":
			if a == nil {
				fail(403, "Forbidden")
				return
			}
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body) == 0 {
				fail(400, "request body cannot be empty")
				return
			}
			f.patches++
			if v, ok := body["note"]; ok {
				a.Note = rawString(v)
			}
			if v, ok := body["name"]; ok {
				a.Name = rawString(v)
			}
			if v, ok := body["mailbox_ids"]; ok {
				var ids []int64
				_ = json.Unmarshal(v, &ids)
				for _, id := range ids {
					if !f.verifiedMailbox(id) {
						fail(400, "Forbidden")
						return
					}
				}
				a.MailboxIDs = ids
			}
			reply(200, map[string]bool{"ok": true})
		case r.Method == http.MethodPost && action == "toggle":
			if a == nil {
				fail(403, "Forbidden")
				return
			}
			if f.failToggles {
				fail(502, "Bad gateway")
				return
			}
			f.toggles++
			a.Enabled = !a.Enabled
			reply(200, map[string]bool{"enabled": a.Enabled})
		case r.Method == http.MethodDelete && action == "":
			if a == nil {
				fail(403, "Forbidden")
				return
			}
			f.deletes++
			f.trash[a.Email] = true
			delete(f.aliases, id)
			reply(200, map[string]bool{"deleted": true})
		default:
			fail(405, "method not allowed")
		}

	default:
		f.t.Errorf("fake SimpleLogin: unexpected request %s %s", r.Method, p)
		fail(404, "not found")
	}
}

func (f *fakeSimpleLogin) findLocked(email string) *fakeAlias {
	for _, a := range f.aliases {
		if a.Email == email {
			return a
		}
	}
	return nil
}

func (f *fakeSimpleLogin) verifiedMailbox(id int64) bool {
	for _, m := range f.mailboxes {
		if m.ID == id {
			return m.Verified
		}
	}
	return false
}

func (f *fakeSimpleLogin) serialize(a *fakeAlias) map[string]any {
	var mbs []map[string]any
	for _, id := range a.MailboxIDs {
		for _, m := range f.mailboxes {
			if m.ID == id {
				mbs = append(mbs, map[string]any{"id": m.ID, "email": m.Email})
			}
		}
	}
	// Upstream builds the list from a Python set: order is not stable.
	rand.Shuffle(len(mbs), func(i, j int) { mbs[i], mbs[j] = mbs[j], mbs[i] })
	return map[string]any{
		"id": a.ID, "email": a.Email, "note": a.Note, "name": a.Name, "enabled": a.Enabled,
		"creation_date": "2020-04-06 17:57:14+00:00", "creation_timestamp": 1586195834,
		"mailbox": mbs[0], "mailboxes": mbs, "latest_activity": nil,
		"nb_block": 0, "nb_forward": 0, "nb_reply": 0, "pinned": false, "support_pgp": false, "disable_pgp": false,
	}
}

func rawString(v json.RawMessage) *string {
	var s *string
	_ = json.Unmarshal(v, &s)
	return s
}
