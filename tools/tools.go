// SPDX-License-Identifier: MPL-2.0

//go:build generate

package tools

import (
	_ "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
)

// Generate documentation from the provider schema and ../examples.
// tfplugindocs needs a Terraform-compatible CLI to read the schema; it
// downloads one if none is on PATH.
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-dir .. --provider-name simplelogin --rendered-provider-name SimpleLogin
