// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Live acceptance tests against a real SimpleLogin account.
//
// They only run when ALL of these are set:
//
//	TF_ACC=1
//	SIMPLELOGIN_API_KEY            API key of the test account
//	SIMPLELOGIN_ACC_DOMAIN         a verified custom domain reserved for tests
//	SIMPLELOGIN_ACC_MAILBOX_EMAIL  a verified mailbox of that account
//
// Each run creates one alias named tfacc-<random>@$SIMPLELOGIN_ACC_DOMAIN and
// deletes it at the end. SimpleLogin keeps deleted addresses in the domain
// trash, so every run leaves one trashed address behind on the test domain.
// Existing aliases are never touched.
func testAccPreCheck(t *testing.T) (domain, mailbox string) {
	t.Helper()
	domain = os.Getenv("SIMPLELOGIN_ACC_DOMAIN")
	mailbox = os.Getenv("SIMPLELOGIN_ACC_MAILBOX_EMAIL")
	if os.Getenv(resource.EnvTfAcc) == "" || os.Getenv(envAPIKey) == "" || domain == "" || mailbox == "" {
		t.Skip("live acceptance test: set TF_ACC=1, SIMPLELOGIN_API_KEY, SIMPLELOGIN_ACC_DOMAIN and SIMPLELOGIN_ACC_MAILBOX_EMAIL to run")
	}
	return domain, mailbox
}

func TestAccAlias(t *testing.T) {
	domain, mailbox := testAccPreCheck(t)
	email := fmt.Sprintf("tfacc-%s@%s", strings.ToLower(acctest.RandString(10)), domain)

	config := func(extra string) string {
		return fmt.Sprintf(`
terraform {
  required_providers {
    simplelogin = { source = "registry.terraform.io/qnimbus/simplelogin" }
  }
}

data "simplelogin_mailbox" "test" {
  email = %q
}

data "simplelogin_domain" "test" {
  domain = %q
}

resource "simplelogin_alias" "test" {
  email       = %q
  mailbox_ids = [data.simplelogin_mailbox.test.id]
  %s
}

data "simplelogin_alias" "test" {
  id = simplelogin_alias.test.id
}
`, mailbox, domain, email, extra)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(`note = "terraform-provider-simplelogin acceptance test"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(aliasAddr, "email", email),
					resource.TestCheckResourceAttr(aliasAddr, "enabled", "true"),
					resource.TestCheckResourceAttr("data.simplelogin_domain.test", "is_verified", "true"),
					resource.TestCheckResourceAttrPair("data.simplelogin_alias.test", "email", aliasAddr, "email"),
				),
			},
			{
				Config: config(`
  note    = "updated"
  name    = "TF Acc"
  enabled = false`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(aliasAddr, "enabled", "false"),
					resource.TestCheckResourceAttr(aliasAddr, "note", "updated"),
					resource.TestCheckResourceAttr(aliasAddr, "name", "TF Acc"),
				),
			},
			{
				ResourceName:      aliasAddr,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
