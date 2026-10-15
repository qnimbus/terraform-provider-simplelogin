// SPDX-License-Identifier: MPL-2.0

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// CustomDomain is a custom domain, as returned by GET /api/custom_domains.
type CustomDomain struct {
	ID                     int64        `json:"id"`
	DomainName             string       `json:"domain_name"`
	IsVerified             bool         `json:"is_verified"`
	CatchAll               bool         `json:"catch_all"`
	Name                   *string      `json:"name"`
	RandomPrefixGeneration bool         `json:"random_prefix_generation"`
	Mailboxes              []MailboxRef `json:"mailboxes"`
	CreationTimestamp      int64        `json:"creation_timestamp"`
}

// ListCustomDomains returns the account's custom domains.
func (c *Client) ListCustomDomains(ctx context.Context) ([]CustomDomain, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/custom_domains", nil, &raw); err != nil {
		return nil, err
	}
	// Upstream returns {"custom_domains": [...]}; the API docs show a bare
	// array. Accept both.
	var domains []CustomDomain
	if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &domains); err != nil {
			return nil, fmt.Errorf("decoding custom domains: %w", err)
		}
		return domains, nil
	}
	var out struct {
		CustomDomains []CustomDomain `json:"custom_domains"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decoding custom domains: %w", err)
	}
	return out.CustomDomains, nil
}

// FindCustomDomain looks up a custom domain by name (case-insensitive).
func (c *Client) FindCustomDomain(ctx context.Context, domain string) (*CustomDomain, error) {
	domains, err := c.ListCustomDomains(ctx)
	if err != nil {
		return nil, err
	}
	for i := range domains {
		if strings.EqualFold(domains[i].DomainName, domain) {
			return &domains[i], nil
		}
	}
	return nil, fmt.Errorf("custom domain %q: %w", domain, ErrNotFound)
}
