// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"regexp"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestDataSources(t *testing.T) {
	f := newFakeSimpleLogin(t)
	id := f.addAlias("existing@example.com", 2, 1)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: f.providerConfig() + `
data "simplelogin_mailbox" "primary" {
  email = "PRIMARY@example.net"
}

data "simplelogin_domain" "example" {
  domain = "example.com"
}

data "simplelogin_alias" "existing" {
  id = "` + strconv.FormatInt(id, 10) + `"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.simplelogin_mailbox.primary", "id", "1"),
				resource.TestCheckResourceAttr("data.simplelogin_mailbox.primary", "verified", "true"),
				resource.TestCheckResourceAttr("data.simplelogin_mailbox.primary", "default", "true"),
				resource.TestCheckResourceAttr("data.simplelogin_domain.example", "id", "1"),
				resource.TestCheckResourceAttr("data.simplelogin_domain.example", "is_verified", "true"),
				resource.TestCheckResourceAttr("data.simplelogin_domain.example", "random_prefix_generation", "true"),
				resource.TestCheckNoResourceAttr("data.simplelogin_domain.example", "name"),
				resource.TestCheckTypeSetElemAttr("data.simplelogin_domain.example", "mailbox_ids.*", "1"),
				resource.TestCheckResourceAttr("data.simplelogin_alias.existing", "email", "existing@example.com"),
				resource.TestCheckResourceAttr("data.simplelogin_alias.existing", "enabled", "true"),
				resource.TestCheckResourceAttr("data.simplelogin_alias.existing", "mailbox_ids.#", "2"),
				resource.TestCheckNoResourceAttr("data.simplelogin_alias.existing", "note"),
			),
		}},
	})
	if n := f.mutations(); n != 0 {
		t.Errorf("data sources made %d mutating API calls", n)
	}
}

func TestDataSourcesNotFound(t *testing.T) {
	f := newFakeSimpleLogin(t)
	for name, cfg := range map[string]struct{ hcl, err string }{
		"mailbox": {`data "simplelogin_mailbox" "m" { email = "nobody@example.net" }`, `SimpleLogin mailbox not found`},
		"domain":  {`data "simplelogin_domain" "d" { domain = "nope.example" }`, `custom domain not found`},
		"alias":   {`data "simplelogin_alias" "a" { id = "424242" }`, `does not exist or is not accessible`},
		"bad id":  {`data "simplelogin_alias" "a" { id = "abc" }`, `positive integer`},
	} {
		t.Run(name, func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testProtoV6ProviderFactories,
				Steps: []resource.TestStep{{
					Config:      f.providerConfig() + cfg.hcl,
					ExpectError: regexp.MustCompile(cfg.err),
				}},
			})
		})
	}
}
