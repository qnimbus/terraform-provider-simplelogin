// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// MailboxRef is the mailbox summary embedded in alias responses.
type MailboxRef struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
}

// Alias is the alias object returned by GET /api/aliases/:id and friends.
type Alias struct {
	ID                int64        `json:"id"`
	Email             string       `json:"email"`
	Name              *string      `json:"name"`
	Note              *string      `json:"note"`
	Enabled           bool         `json:"enabled"`
	CreationTimestamp int64        `json:"creation_timestamp"`
	Pinned            bool         `json:"pinned"`
	Mailboxes         []MailboxRef `json:"mailboxes"`
}

// MailboxRefIDs returns the IDs of refs, sorted and de-duplicated. The API
// returns mailboxes in no particular order.
func MailboxRefIDs(refs []MailboxRef) []int64 {
	ids := make([]int64, 0, len(refs))
	for _, m := range refs {
		ids = append(ids, m.ID)
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// MailboxIDs returns the alias' mailbox IDs, sorted and de-duplicated.
func (a *Alias) MailboxIDs() []int64 { return MailboxRefIDs(a.Mailboxes) }

// AliasSuffix is one entry of GET /api/v5/alias/options "suffixes".
type AliasSuffix struct {
	Suffix       string `json:"suffix"`
	SignedSuffix string `json:"signed_suffix"`
	IsCustom     bool   `json:"is_custom"`
	IsPremium    bool   `json:"is_premium"`
}

// AliasOptions is the response of GET /api/v5/alias/options.
type AliasOptions struct {
	CanCreate bool          `json:"can_create"`
	Suffixes  []AliasSuffix `json:"suffixes"`
}

// CreateAliasRequest is the body of POST /api/v3/alias/custom/new.
type CreateAliasRequest struct {
	AliasPrefix  string  `json:"alias_prefix"`
	SignedSuffix string  `json:"signed_suffix"`
	MailboxIDs   []int64 `json:"mailbox_ids"`
	Note         *string `json:"note,omitempty"`
	Name         *string `json:"name,omitempty"`
}

// AliasUpdate is the body of PATCH /api/aliases/:id. Only non-nil fields are
// sent. Set*Null clears the corresponding field.
type AliasUpdate struct {
	Note       *string
	ClearNote  bool
	Name       *string
	ClearName  bool
	MailboxIDs []int64
}

func (u AliasUpdate) body() map[string]any {
	b := map[string]any{}
	switch {
	case u.ClearNote:
		b["note"] = nil
	case u.Note != nil:
		b["note"] = *u.Note
	}
	switch {
	case u.ClearName:
		b["name"] = nil
	case u.Name != nil:
		b["name"] = *u.Name
	}
	if u.MailboxIDs != nil {
		b["mailbox_ids"] = u.MailboxIDs
	}
	return b
}

func aliasPath(id int64) string { return "/api/aliases/" + strconv.FormatInt(id, 10) }

// GetAlias fetches an alias by ID.
//
// SimpleLogin answers a request for a non-existent alias with HTTP 400
// "Unknown error" instead of 404. Because a 400 is not a reliable deletion
// signal on its own, GetAlias only returns ErrNotFound when knownEmail is set
// and a search of the account's aliases for that address confirms that no
// alias with this ID exists. HTTP 404 (should the API ever return it) is also
// treated as not found. Every other failure is returned as an error.
func (c *Client) GetAlias(ctx context.Context, id int64, knownEmail string) (*Alias, error) {
	var a Alias
	err := c.do(ctx, http.MethodGet, aliasPath(id), nil, &a)
	if err == nil {
		return &a, nil
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return nil, err
	}
	switch {
	case apiErr.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("alias %d: %w", id, ErrNotFound)
	case apiErr.StatusCode == http.StatusBadRequest && knownEmail != "":
		found, lerr := c.aliasListed(ctx, id, knownEmail)
		if lerr != nil {
			return nil, fmt.Errorf("%w; additionally, confirming whether the alias still exists failed: %w", err, lerr)
		}
		if !found {
			return nil, fmt.Errorf("alias %d (%s): %w", id, knownEmail, ErrNotFound)
		}
		return nil, err
	case apiErr.StatusCode == http.StatusBadRequest:
		return nil, fmt.Errorf("alias %d does not exist or is not accessible with this API key: %w", id, err)
	}
	return nil, err
}

// maxSearchPages bounds the number of pages read when searching aliases.
const maxSearchPages = 50

// SearchAliases returns one page (max 20) of the account's active aliases
// matching query. Trashed aliases are not included by the API.
func (c *Client) SearchAliases(ctx context.Context, query string, page int) ([]Alias, error) {
	var out struct {
		Aliases []Alias `json:"aliases"`
	}
	q := url.Values{"page_id": {strconv.Itoa(page)}}
	// The API takes the search query in the body. POST is the documented
	// alternative to GET-with-body; it is read-only, so it is safe to retry.
	if err := c.request(ctx, http.MethodPost, "/api/v2/aliases", q, map[string]string{"query": query}, &out, true); err != nil {
		return nil, err
	}
	return out.Aliases, nil
}

func (c *Client) aliasListed(ctx context.Context, id int64, email string) (bool, error) {
	for page := 0; page < maxSearchPages; page++ {
		aliases, err := c.SearchAliases(ctx, email, page)
		if err != nil {
			return false, err
		}
		if len(aliases) == 0 {
			return false, nil
		}
		for _, a := range aliases {
			if a.ID == id {
				return true, nil
			}
			// Addresses are unique: if another alias holds it, ours is gone.
			if strings.EqualFold(a.Email, email) {
				return false, nil
			}
		}
	}
	return false, fmt.Errorf("searching aliases for %q exceeded %d pages", email, maxSearchPages)
}

// GetAliasOptions returns the suffixes the account can use to create aliases.
// Signed suffixes expire after a few minutes upstream, so fetch them right
// before creating an alias.
func (c *Client) GetAliasOptions(ctx context.Context) (*AliasOptions, error) {
	var o AliasOptions
	if err := c.do(ctx, http.MethodGet, "/api/v5/alias/options", nil, &o); err != nil {
		return nil, err
	}
	return &o, nil
}

// CreateCustomAlias creates an alias. It is never retried: a lost response
// does not mean the alias was not created.
func (c *Client) CreateCustomAlias(ctx context.Context, req CreateAliasRequest) (*Alias, error) {
	var a Alias
	if err := c.do(ctx, http.MethodPost, "/api/v3/alias/custom/new", req, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// UpdateAlias patches the alias. A no-op update sends nothing.
func (c *Client) UpdateAlias(ctx context.Context, id int64, u AliasUpdate) error {
	body := u.body()
	if len(body) == 0 {
		return nil
	}
	return c.do(ctx, http.MethodPatch, aliasPath(id), body, nil)
}

// ToggleAlias flips the enabled state and returns the new state. The endpoint
// is not idempotent: callers must check the current state first and must not
// retry it blindly.
func (c *Client) ToggleAlias(ctx context.Context, id int64) (bool, error) {
	var out struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.do(ctx, http.MethodPost, aliasPath(id)+"/toggle", nil, &out); err != nil {
		return false, err
	}
	if out.Enabled == nil {
		return false, fmt.Errorf("toggle alias %d: response did not include the new enabled state", id)
	}
	return *out.Enabled, nil
}

// DeleteAlias deletes the alias. Upstream moves deleted custom-domain aliases
// to the domain trash, after which the same address cannot be created again
// through the API.
func (c *Client) DeleteAlias(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodDelete, aliasPath(id), nil, nil)
}

// aliasLocalPart mirrors upstream's accepted alias prefix: [0-9a-z-_.], at
// most 40 characters (app/alias_utils.py check_alias_prefix). Upstream also
// lowercases and rewrites other characters, so anything outside this set
// would produce a different address than requested.
var aliasLocalPart = regexp.MustCompile(`^[0-9a-z_.-]{1,40}$`)

// SplitAliasEmail validates an alias address and splits it into the prefix
// and domain that SimpleLogin would use to create exactly that address.
func SplitAliasEmail(email string) (prefix, domain string, err error) {
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "@") {
		return "", "", fmt.Errorf("%q is not an email address", email)
	}
	if !aliasLocalPart.MatchString(local) {
		return "", "", fmt.Errorf("the local part of %q must be 1-40 characters of lowercase a-z, 0-9, '.', '_' or '-'", email)
	}
	if strings.HasPrefix(local, ".") || strings.HasSuffix(local, ".") || strings.Contains(local, "..") {
		return "", "", fmt.Errorf("the local part of %q must not start or end with '.' or contain '..'", email)
	}
	if domain != strings.ToLower(domain) {
		return "", "", fmt.Errorf("the domain of %q must be lowercase", email)
	}
	return local, domain, nil
}

// FindExactSuffix returns the suffix option that yields exactly
// "<prefix>@<domain>", i.e. the suffix "@<domain>". SimpleLogin offers such a
// suffix for each verified custom domain (and, on self-hosted instances with
// DISABLE_ALIAS_SUFFIX, for built-in domains). Suffixes with a random word
// (".word@domain") are never used because they cannot produce the requested
// address.
func FindExactSuffix(opts *AliasOptions, domain string) (*AliasSuffix, error) {
	want := "@" + domain
	for i := range opts.Suffixes {
		if strings.EqualFold(opts.Suffixes[i].Suffix, want) {
			return &opts.Suffixes[i], nil
		}
	}
	var avail []string
	for _, s := range opts.Suffixes {
		if strings.HasPrefix(s.Suffix, "@") {
			avail = append(avail, strings.TrimPrefix(s.Suffix, "@"))
		}
	}
	msg := fmt.Sprintf("domain %q cannot be used to create an alias with an exact address: it is not a verified custom domain of this SimpleLogin account", domain)
	if len(avail) > 0 {
		msg += fmt.Sprintf(" (usable domains: %s)", strings.Join(avail, ", "))
	} else {
		msg += " (the account has no domains usable for exact addresses)"
	}
	return nil, errors.New(msg)
}
