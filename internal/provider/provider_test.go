// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// testProtoV6ProviderFactories serves the provider in-process for the
// Terraform/OpenTofu CLI used by terraform-plugin-testing. Set
// TF_ACC_TERRAFORM_PATH to an OpenTofu binary to test with OpenTofu.
var testProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"simplelogin": providerserver.NewProtocol6WithError(New("test")()),
}

func init() {
	// Serve the provider under its canonical address
	// registry.terraform.io/qnimbus/simplelogin, which test configurations
	// reference explicitly so that Terraform and OpenTofu agree on it.
	if os.Getenv(resource.EnvTfAccProviderNamespace) == "" {
		_ = os.Setenv(resource.EnvTfAccProviderNamespace, "qnimbus")
	}
}

func TestProviderMissingAPIKey(t *testing.T) {
	t.Setenv("SIMPLELOGIN_API_KEY", "")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: `
terraform {
  required_providers {
    simplelogin = { source = "registry.terraform.io/qnimbus/simplelogin" }
  }
}
provider "simplelogin" {}
data "simplelogin_mailbox" "m" { email = "a@example.net" }
`,
			ExpectError: regexp.MustCompile(`Missing SimpleLogin API key`),
		}},
	})
}

func TestProviderInvalidAPIKey(t *testing.T) {
	f := newFakeSimpleLogin(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: `
terraform {
  required_providers {
    simplelogin = { source = "registry.terraform.io/qnimbus/simplelogin" }
  }
}
provider "simplelogin" {
  base_url = "` + f.srv.URL + `"
  api_key  = "wrong-key"
}
data "simplelogin_mailbox" "m" { email = "primary@example.net" }
`,
			ExpectError: regexp.MustCompile(`(?s)HTTP 401.*Wrong api key`),
		}},
	})
}

func TestProviderAPIKeyFromEnv(t *testing.T) {
	f := newFakeSimpleLogin(t)
	t.Setenv("SIMPLELOGIN_API_KEY", fakeAPIKey)
	t.Setenv("SIMPLELOGIN_BASE_URL", f.srv.URL)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: `
terraform {
  required_providers {
    simplelogin = { source = "registry.terraform.io/qnimbus/simplelogin" }
  }
}
data "simplelogin_mailbox" "m" { email = "primary@example.net" }
`,
			Check: resource.TestCheckResourceAttr("data.simplelogin_mailbox.m", "id", "1"),
		}},
	})
}
