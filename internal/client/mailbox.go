// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// Mailbox is a forwarding destination, as returned by GET /api/v2/mailboxes.
type Mailbox struct {
	ID       int64  `json:"id"`
	Email    string `json:"email"`
	Verified bool   `json:"verified"`
	Default  bool   `json:"default"`
}

// ListMailboxes returns all mailboxes of the account, including unverified
// ones.
func (c *Client) ListMailboxes(ctx context.Context) ([]Mailbox, error) {
	var out struct {
		Mailboxes []Mailbox `json:"mailboxes"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v2/mailboxes", nil, &out); err != nil {
		return nil, err
	}
	return out.Mailboxes, nil
}

// FindMailboxByEmail looks up a mailbox by address (case-insensitive).
func (c *Client) FindMailboxByEmail(ctx context.Context, email string) (*Mailbox, error) {
	mailboxes, err := c.ListMailboxes(ctx)
	if err != nil {
		return nil, err
	}
	for i := range mailboxes {
		if strings.EqualFold(mailboxes[i].Email, email) {
			return &mailboxes[i], nil
		}
	}
	return nil, fmt.Errorf("mailbox %q: %w", email, ErrNotFound)
}
