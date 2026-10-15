// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const aliasAddr = "simplelogin_alias.test"

// checkRemote asserts on the fake server's copy of the alias.
func checkRemote(f *fakeSimpleLogin, email string, check func(a *fakeAlias) error) resource.TestCheckFunc {
	return func(*terraform.State) error {
		a := f.aliasByEmail(email)
		if a == nil {
			return fmt.Errorf("alias %s does not exist remotely", email)
		}
		return check(a)
	}
}

func TestAliasResourceLifecycle(t *testing.T) {
	f := newFakeSimpleLogin(t)
	const email = "admin@example.com"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if f.aliasByEmail(email) != nil {
				return fmt.Errorf("alias %s still exists", email)
			}
			return nil
		},
		Steps: []resource.TestStep{
			// Create with defaults.
			{
				Config: f.providerConfig() + `
data "simplelogin_mailbox" "primary" {
  email = "primary@example.net"
}

resource "simplelogin_alias" "test" {
  email       = "admin@example.com"
  mailbox_ids = [data.simplelogin_mailbox.primary.id]
  note        = "Administrative email alias"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(aliasAddr, "id"),
					resource.TestCheckResourceAttr(aliasAddr, "email", email),
					resource.TestCheckResourceAttr(aliasAddr, "enabled", "true"),
					resource.TestCheckResourceAttr(aliasAddr, "note", "Administrative email alias"),
					resource.TestCheckNoResourceAttr(aliasAddr, "name"),
					resource.TestCheckResourceAttr(aliasAddr, "mailbox_ids.#", "1"),
					resource.TestCheckTypeSetElemAttr(aliasAddr, "mailbox_ids.*", "1"),
					checkRemote(f, email, func(a *fakeAlias) error {
						if a.Note == nil || *a.Note != "Administrative email alias" || !a.Enabled {
							return fmt.Errorf("remote alias = %+v", a)
						}
						return nil
					}),
				),
			},
			// Update in place: second mailbox, name, note and disable.
			{
				Config: f.providerConfig() + `
resource "simplelogin_alias" "test" {
  email       = "admin@example.com"
  mailbox_ids = [2, 1]
  note        = "changed"
  name        = "Admin"
  enabled     = false
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(aliasAddr, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(aliasAddr, "enabled", "false"),
					resource.TestCheckResourceAttr(aliasAddr, "name", "Admin"),
					resource.TestCheckResourceAttr(aliasAddr, "mailbox_ids.#", "2"),
					checkRemote(f, email, func(a *fakeAlias) error {
						ids := slices.Sorted(slices.Values(a.MailboxIDs))
						if a.Enabled || *a.Note != "changed" || *a.Name != "Admin" || !slices.Equal(ids, []int64{1, 2}) {
							return fmt.Errorf("remote alias = %+v", a)
						}
						if f.toggles != 1 || f.creates != 1 {
							return fmt.Errorf("toggles=%d creates=%d, want 1/1", f.toggles, f.creates)
						}
						return nil
					}),
				),
			},
			// Re-ordering mailbox IDs is not a change.
			{
				Config: f.providerConfig() + `
resource "simplelogin_alias" "test" {
  email       = "admin@example.com"
  mailbox_ids = [1, 2]
  note        = "changed"
  name        = "Admin"
  enabled     = false
}
`,
				PlanOnly: true,
			},
			// Unset note and name, re-enable.
			{
				Config: f.providerConfig() + `
resource "simplelogin_alias" "test" {
  email       = "admin@example.com"
  mailbox_ids = [1]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(aliasAddr, "note"),
					resource.TestCheckNoResourceAttr(aliasAddr, "name"),
					resource.TestCheckResourceAttr(aliasAddr, "enabled", "true"),
					checkRemote(f, email, func(a *fakeAlias) error {
						if a.Note != nil || a.Name != nil || !a.Enabled || f.toggles != 2 {
							return fmt.Errorf("remote alias = %+v, toggles=%d", a, f.toggles)
						}
						return nil
					}),
				),
			},
			// Import by ID matches state exactly.
			{
				ResourceName:      aliasAddr,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAliasResourceCreateDisabled(t *testing.T) {
	f := newFakeSimpleLogin(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: f.providerConfig() + `
resource "simplelogin_alias" "test" {
  email       = "off@example.com"
  mailbox_ids = [1]
  enabled     = false
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(aliasAddr, "enabled", "false"),
				checkRemote(f, "off@example.com", func(a *fakeAlias) error {
					if a.Enabled || f.toggles != 1 {
						return fmt.Errorf("enabled=%v toggles=%d", a.Enabled, f.toggles)
					}
					return nil
				}),
			),
		}},
	})
}

// Importing an existing alias must not modify or recreate it.
func TestAliasResourceImportExisting(t *testing.T) {
	f := newFakeSimpleLogin(t)
	id := strconv.FormatInt(f.addAlias("legacy@example.com", 2, 1), 10)
	config := f.providerConfig() + `
import {
  to = simplelogin_alias.test
  id = "` + id + `"
}

resource "simplelogin_alias" "test" {
  email       = "legacy@example.com"
  mailbox_ids = [1, 2]
}
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(aliasAddr, plancheck.ResourceActionNoop)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(aliasAddr, "id", id),
					func(*terraform.State) error {
						if n := f.mutations(); n != 0 {
							return fmt.Errorf("import made %d mutating API calls", n)
						}
						return nil
					},
				),
			},
		},
	})
}

func TestAliasResourceImportInvalidID(t *testing.T) {
	f := newFakeSimpleLogin(t)
	for id, want := range map[string]string{"abc": "positive integer", "999999": "does not exist or is not accessible"} {
		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testProtoV6ProviderFactories,
			Steps: []resource.TestStep{{
				Config: f.providerConfig() + `
import {
  to = simplelogin_alias.test
  id = "` + id + `"
}

resource "simplelogin_alias" "test" {
  email       = "x@example.com"
  mailbox_ids = [1]
}
`,
				ExpectError: regexp.MustCompile(want),
			}},
		})
	}
}

// An alias deleted outside OpenTofu is dropped from state on refresh, but a
// transient API failure is an error and keeps the alias in state.
func TestAliasResourceRefresh(t *testing.T) {
	f := newFakeSimpleLogin(t)
	config := f.providerConfig() + `
resource "simplelogin_alias" "test" {
  email       = "refresh@example.com"
  mailbox_ids = [1]
}
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig:   func() { f.mu.Lock(); f.failGets = true; f.mu.Unlock() },
				Config:      config,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`HTTP 500`),
			},
			{
				PreConfig: func() { f.mu.Lock(); f.failGets = false; f.mu.Unlock() },
				Config:    config,
				PlanOnly:  true,
			},
			{
				PreConfig: func() {
					f.mu.Lock()
					defer f.mu.Unlock()
					a := f.findLocked("refresh@example.com")
					delete(f.aliases, a.ID)
				},
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestAliasResourceEmailChangeReplaces(t *testing.T) {
	f := newFakeSimpleLogin(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: f.providerConfig() + `
resource "simplelogin_alias" "test" {
  email       = "old@example.com"
  mailbox_ids = [1]
}
`},
			{
				Config: f.providerConfig() + `
resource "simplelogin_alias" "test" {
  email       = "new@example.com"
  mailbox_ids = [1]
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(aliasAddr, plancheck.ResourceActionDestroyBeforeCreate)},
				},
				Check: func(*terraform.State) error {
					if f.aliasByEmail("old@example.com") != nil || f.aliasByEmail("new@example.com") == nil {
						return fmt.Errorf("replacement did not happen")
					}
					return nil
				},
			},
		},
	})
}

func TestAliasResourceCreateErrors(t *testing.T) {
	f := newFakeSimpleLogin(t)
	f.addAlias("taken@example.com", 1)
	f.trash["trashed@example.com"] = true

	tests := map[string]struct{ email, mailboxes, err string }{
		"existing alias":     {"taken@example.com", "[1]", `(?s)Alias already exists.*import`},
		"trashed alias":      {"trashed@example.com", "[1]", `(?s)Alias already exists.*domain.s\s+trash`},
		"non-custom domain":  {"me@simplelogin.com", "[1]", `(?s)cannot be used to create an alias.*usable domains: example.com`},
		"unknown domain":     {"me@other.example", "[1]", `not a verified custom domain`},
		"unverified mailbox": {"mb@example.com", "[3]", `Errors\s+with\s+Mailbox`},
		"uppercase":          {"Admin@example.com", "[1]", `must be 1-40 characters of lowercase`},
		"plus addressing":    {"a+b@example.com", "[1]", `Invalid alias address`},
		"empty mailboxes":    {"x@example.com", "[]", `at least 1`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProtoV6ProviderFactories,
				Steps: []resource.TestStep{{
					Config: f.providerConfig() + fmt.Sprintf(`
resource "simplelogin_alias" "test" {
  email       = %q
  mailbox_ids = %s
}
`, tt.email, tt.mailboxes),
					ExpectError: regexp.MustCompile(tt.err),
				}},
			})
		})
	}
	if f.creates != 0 {
		t.Errorf("%d aliases created", f.creates)
	}
}

func TestAliasResourceValidation(t *testing.T) {
	f := newFakeSimpleLogin(t)
	for name, tt := range map[string]struct{ attrs, err string }{
		"empty note":      {`note = ""`, `at least 1`},
		"multiline name":  {`name = "a\nb"`, `line breaks`},
		"long name":       {`name = "` + strings.Repeat("a", 129) + `"`, `between 1 and 128`},
		"zero mailbox id": {`mailbox_ids = [0]`, `at least 1`},
	} {
		t.Run(name, func(t *testing.T) {
			mailboxes := "mailbox_ids = [1]"
			if regexp.MustCompile(`mailbox_ids`).MatchString(tt.attrs) {
				mailboxes = ""
			}
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProtoV6ProviderFactories,
				Steps: []resource.TestStep{{
					Config: f.providerConfig() + fmt.Sprintf(`
resource "simplelogin_alias" "test" {
  email = "v@example.com"
  %s
  %s
}
`, mailboxes, tt.attrs),
					PlanOnly:    true,
					ExpectError: regexp.MustCompile(tt.err),
				}},
			})
		})
	}
	if n := f.mutations(); n != 0 {
		t.Errorf("validation made %d mutating API calls", n)
	}
}

// Destroying an alias that is already gone succeeds without calling DELETE.
func TestAliasResourceDeleteAlreadyGone(t *testing.T) {
	f := newFakeSimpleLogin(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: f.providerConfig() + `
resource "simplelogin_alias" "test" {
  email       = "gone@example.com"
  mailbox_ids = [1]
}
`},
			{
				Config: f.providerConfig() + `
resource "simplelogin_alias" "test" {
  email       = "gone@example.com"
  mailbox_ids = [1]
}
`,
				Destroy: true,
				PreConfig: func() {
					f.mu.Lock()
					defer f.mu.Unlock()
					delete(f.aliases, f.findLocked("gone@example.com").ID)
				},
			},
		},
	})
	if f.deletes != 0 {
		t.Errorf("deletes = %d, want 0", f.deletes)
	}
}

// Failures after the alias was created must not taint it: replacing it would
// delete the alias, and the domain trash would then block the address.
func TestAliasResourceCreatePartialFailureDoesNotTaint(t *testing.T) {
	f := newFakeSimpleLogin(t)
	const email = "partial@example.com"
	config := f.providerConfig() + `
resource "simplelogin_alias" "test" {
  email       = "partial@example.com"
  mailbox_ids = [1]
  enabled     = false
}
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Disabling fails: apply succeeds with a warning, the alias is
			// still enabled remotely and the follow-up plan wants to fix it.
			{
				PreConfig:          func() { f.mu.Lock(); f.failToggles = true; f.mu.Unlock() },
				Config:             config,
				ExpectNonEmptyPlan: true,
				Check: checkRemote(f, email, func(a *fakeAlias) error {
					if !a.Enabled {
						return fmt.Errorf("alias unexpectedly disabled")
					}
					return nil
				}),
			},
			// The next plan updates in place; it does not replace.
			{
				PreConfig: func() { f.mu.Lock(); f.failToggles = false; f.mu.Unlock() },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(aliasAddr, plancheck.ResourceActionUpdate)},
				},
				Check: checkRemote(f, email, func(a *fakeAlias) error {
					if a.Enabled || f.creates != 1 || f.deletes != 0 {
						return fmt.Errorf("enabled=%v creates=%d deletes=%d", a.Enabled, f.creates, f.deletes)
					}
					return nil
				}),
			},
		},
	})
}

func TestAliasResourceCreateReadBackFailureDoesNotTaint(t *testing.T) {
	f := newFakeSimpleLogin(t)
	config := f.providerConfig() + `
resource "simplelogin_alias" "test" {
  email       = "readback@example.com"
  mailbox_ids = [1, 2]
  note        = "n"
}
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				PreConfig: func() { f.mu.Lock(); f.failNextGets = 1; f.mu.Unlock() },
				Config:    config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(aliasAddr, "id"),
					resource.TestCheckResourceAttr(aliasAddr, "mailbox_ids.#", "2"),
				),
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: func(*terraform.State) error {
					if f.creates != 1 || f.deletes != 0 {
						return fmt.Errorf("creates=%d deletes=%d", f.creates, f.deletes)
					}
					return nil
				},
			},
		},
	})
}
