# SimpleLogin Provider for OpenTofu / Terraform

Manage [SimpleLogin](https://simplelogin.io) email aliases and their forwarding
mailboxes as code. Built with the
[Terraform Plugin Framework](https://developer.hashicorp.com/terraform/plugin/framework)
and tested with [OpenTofu](https://opentofu.org).

- Provider source address: **`registry.terraform.io/qnimbus/simplelogin`**
- Status: **0.1 MVP**, not yet published to any registry. Install it from GitHub
  releases through a filesystem mirror (see [Installing a release](#installing-a-release)).

> [!WARNING]
> **Deleting an alias cannot be undone through the API.** SimpleLogin moves a
> deleted custom-domain alias into the domain's *trash* and refuses to create
> that address again (HTTP 409) while it is there. The domain trash has no
> restore action; the alias, its contacts and history are gone, and the
> address can only be freed by removing it from the trash in the dashboard. Destroying a
> `simplelogin_alias`, or changing its `email` (which forces replacement),
> deletes the alias. Put `lifecycle { prevent_destroy = true }` on every
> address you depend on, and check every plan for `destroy` / `must be replaced`.

## Features

| Kind        | Name                  | What it does                                                                       |
|-------------|-----------------------|------------------------------------------------------------------------------------|
| Resource    | `simplelogin_alias`   | Create, update, import and delete an alias on a verified custom domain             |
| Data source | `simplelogin_alias`   | Read an alias by ID                                                                |
| Data source | `simplelogin_mailbox` | Look up a mailbox (forwarding destination) ID by email address                     |
| Data source | `simplelogin_domain`  | Read a custom domain by name                                                       |

The `simplelogin_alias` resource manages:

| Attribute     | Type          | Behaviour                                                                 |
|---------------|---------------|---------------------------------------------------------------------------|
| `id`          | string        | Computed SimpleLogin alias ID; also the import ID                          |
| `email`       | string        | Required. Exact address; changing it **replaces** (deletes) the alias      |
| `mailbox_ids` | set of number | Required, ≥ 1. Forwarding mailboxes, updated in place; order is irrelevant |
| `note`        | string        | Optional. Updated in place; removing it clears the note                    |
| `name`        | string        | Optional, ≤ 128 chars. Display name; updated in place                      |
| `enabled`     | bool          | Optional, default `true`. Updated in place                                 |
| `on_destroy`  | string        | Optional, `"delete"` (default) or `"disable"`: what destroying does        |

Full reference: [`docs/`](docs/index.md) (generated from the schemas).

### Not supported

- Aliases on SimpleLogin's shared domains (`@simplelogin.com`, `@aleeas.com`, …).
  Their addresses always contain a server-chosen random word, so the provider
  cannot create a specific address there. You can still **import** such aliases
  and manage their mailboxes, note, name and enabled state.
- Random aliases, contacts/reverse aliases, PGP settings and pinning.
- Creating, changing or deleting mailboxes, custom domains or DNS records.
  Mailboxes and domains are read-only through data sources. DNS stays with
  your DNS tooling (e.g. DNSControl).
- Managing the alias trash. Use the SimpleLogin dashboard for that.

## Requirements

- An API key from the SimpleLogin dashboard (**API Keys** page). An API key has
  full access to its account. SimpleLogin has no scoped keys, so create a
  dedicated key for automation and revoke it when no longer needed.
- For creating aliases: a custom domain that is **verified** in SimpleLogin, and
  at least one **verified** mailbox. The plan must allow creating aliases.
- OpenTofu 1.6 or later (`removed` blocks need 1.7). Tested with OpenTofu 1.13.1.
  Terraform should also work but is not part of the test setup.

## Usage

```hcl
terraform {
  required_providers {
    simplelogin = {
      source  = "registry.terraform.io/qnimbus/simplelogin"
      version = "~> 0.1.0"
    }
  }
}

# API key from SIMPLELOGIN_API_KEY.
provider "simplelogin" {}

data "simplelogin_mailbox" "primary" {
  email = "personal@example.net"
}

resource "simplelogin_alias" "admin" {
  email       = "admin@example.com"
  mailbox_ids = [data.simplelogin_mailbox.primary.id]
  note        = "Administrative email alias"
  enabled     = true

  lifecycle {
    prevent_destroy = true
  }
}
```

> [!IMPORTANT]
> Always write the full source address `registry.terraform.io/qnimbus/simplelogin`.
> In OpenTofu the short form `qnimbus/simplelogin` means
> `registry.opentofu.org/qnimbus/simplelogin`, which is a different provider address.

### Provider configuration

| Argument   | Environment variable   | Default                      |
|------------|------------------------|------------------------------|
| `api_key`  | `SIMPLELOGIN_API_KEY`  | none (required)              |
| `base_url` | `SIMPLELOGIN_BASE_URL` | `https://app.simplelogin.io` |

Prefer the environment variable for the API key so it never appears in
configuration. The key is sent only in the `Authentication` header. It is never
logged, never written to state and never included in error messages.
`base_url` is the instance root without `/api`, so a self-hosted instance works
too.

### Importing existing aliases

Adopting an alias never modifies it:

1. Find the alias ID: open the alias in the SimpleLogin dashboard, or query
   `GET /api/v2/aliases?page_id=0`.
2. Add an `import` block and a resource block that describes the alias **as it
   currently is**:

   ```hcl
   import {
     to = simplelogin_alias.admin
     id = "12345"
   }

   resource "simplelogin_alias" "admin" {
     email       = "admin@example.com"
     mailbox_ids = [data.simplelogin_mailbox.primary.id]
     note        = "existing note, if any"

     lifecycle {
       prevent_destroy = true
     }
   }
   ```

3. Run `tofu plan`. It must read `1 to import, 0 to add, 0 to change, 0 to destroy`.
   If it shows changes, adjust the resource block until it doesn't, so that the
   apply doesn't alter mail delivery. The one harmless exception is
   `on_destroy = "disable"`. SimpleLogin doesn't store it, so an import starts at
   `"delete"` and the plan shows `on_destroy` changing in place. That change
   makes no API call.
4. Run `tofu apply`. Afterwards you can remove the `import` block.

`tofu import simplelogin_alias.admin 12345` also works.

Creating an alias whose address already exists fails with *Alias already
exists* and suggests importing it. The provider never silently adopts or
overwrites an existing alias.

### Removing an alias from management without deleting it

```hcl
removed {
  from = simplelogin_alias.admin
  lifecycle {
    destroy = false
  }
}
```

(or `tofu state rm simplelogin_alias.admin`).

### Disabling instead of deleting on destroy

By default, destroying a `simplelogin_alias` deletes the alias, and so does
replacing it because `email` changed. With `on_destroy = "disable"` the alias
is only **disabled** and stays in SimpleLogin, so its address is never lost to
the domain trash:

```hcl
resource "simplelogin_alias" "admin" {
  email       = "admin@example.com"
  mailbox_ids = [data.simplelogin_mailbox.primary.id]
  on_destroy  = "disable"
}
```

- The plan still says `destroy`. The apply prints a warning that the alias was
  only disabled, with its ID.
- The disabled alias still holds its address. Applying the same configuration
  again fails with *Alias already exists*. Import the alias by ID instead, and
  it is re-enabled because `enabled` defaults to `true`.
- The setting lives only in state, and changing it makes no API call. The value
  **in state** at destroy time counts, so apply `on_destroy = "disable"` first,
  then remove the resource or change its `email`.
- This doesn't replace `prevent_destroy`, which stops the destroy outright.
  `on_destroy` decides what a destroy does once it is allowed. To stop
  managing an alias and leave it fully untouched (still enabled), use the
  `removed` block above.

## Installing a release

GitHub releases contain `terraform-provider-simplelogin_<version>_<os>_<arch>.zip`
archives (linux_amd64, linux_arm64, windows_amd64) and a `SHA256SUMS` file.
`tofu init` **cannot** install a provider directly from a GitHub URL. Releases
are consumed through an OpenTofu
[filesystem mirror](https://opentofu.org/docs/cli/config/config-file/#provider-installation)
instead, which works with ordinary `version` constraints and the lock file, and
needs no `dev_overrides`.

### Consuming the provider from another repository (e.g. `qnimbus/infrastructure`)

A complete example is in [`examples/infrastructure/`](examples/infrastructure).

1. Download the release into a mirror inside the infrastructure repository. The
   script verifies SHA256 checksums. Add every platform that runs OpenTofu:

   ```bash
   curl -fsSLO https://raw.githubusercontent.com/qnimbus/terraform-provider-simplelogin/v0.1.0/scripts/install-provider-mirror.sh
   bash install-provider-mirror.sh 0.1.0 "$PWD/.terraform-mirror" linux_amd64 linux_arm64
   ```

   This creates
   `.terraform-mirror/registry.terraform.io/qnimbus/simplelogin/terraform-provider-simplelogin_0.1.0_linux_amd64.zip`
   (the mirror's "packed" layout). You can either commit the mirror directory
   or recreate it in CI.

2. Create a CLI configuration (based on
   [`tofurc.example`](examples/infrastructure/tofurc.example)) that takes this
   provider from the mirror and everything else from the registry:

   ```hcl
   provider_installation {
     filesystem_mirror {
       path    = "/absolute/path/to/infrastructure/.terraform-mirror"
       include = ["registry.terraform.io/qnimbus/simplelogin"]
     }
     direct {
       exclude = ["registry.terraform.io/qnimbus/simplelogin"]
     }
   }
   ```

   ```bash
   export TF_CLI_CONFIG_FILE="$PWD/.tofurc"
   ```

3. Declare the provider with `source = "registry.terraform.io/qnimbus/simplelogin"`
   and `version = "~> 0.1.0"`, then:

   ```bash
   tofu init
   # Record checksums for all platforms in .terraform.lock.hcl and commit it:
   tofu providers lock -fs-mirror="$PWD/.terraform-mirror" \
     -platform=linux_amd64 -platform=linux_arm64
   ```

To upgrade, install the new version into the mirror, bump the constraint and run
`tofu init -upgrade` plus `tofu providers lock …` again.

### Ownership split with DNSControl

OpenTofu owns the SimpleLogin aliases and their forwarding configuration.
DNSControl is the only authority for DNS: MX, SPF, DKIM, DMARC and the
SimpleLogin ownership record belong in DNSControl, not in OpenTofu. This
provider does not manage DNS.

## Development

Tools are pinned with [mise](https://mise.jdx.dev) in `.mise/config.toml` (Go,
OpenTofu). After `mise install`:

| Command                 | Purpose                                                                    |
|-------------------------|----------------------------------------------------------------------------|
| `mise run build`        | Build `bin/terraform-provider-simplelogin`                                 |
| `mise run check`        | gofmt, `tofu fmt -check`, `go mod tidy -diff`, `go vet`                    |
| `mise run test`         | Unit and provider tests (mock API, real OpenTofu); no credentials needed   |
| `mise run testacc`      | **Live** acceptance tests (see below)                                      |
| `mise run docs`         | Regenerate `docs/` from schemas and `examples/`                            |
| `mise run dev-validate` | `tofu validate` the examples with the locally built provider               |
| `mise run dev-plan`     | `tofu plan` the smoke-test example with the locally built provider         |

### Running a local build with OpenTofu (dev overrides)

```bash
mise run dev-tofurc                       # builds ./bin and writes bin/dev.tofurc
export TF_CLI_CONFIG_FILE="$PWD/bin/dev.tofurc"
cd examples/dev
SIMPLELOGIN_API_KEY=placeholder tofu plan -refresh=false   # planning a new alias makes no API calls
```

`bin/dev.tofurc` maps `registry.terraform.io/qnimbus/simplelogin` to `./bin`:

```hcl
provider_installation {
  dev_overrides {
    "registry.terraform.io/qnimbus/simplelogin" = "/path/to/terraform-provider-simplelogin/bin"
  }
  direct {}
}
```

Notes on dev overrides: skip `tofu init` for configurations that only use this
provider. `init` would try to resolve the provider from the registry, where it
doesn't exist. Overrides also bypass version constraints and the lock file.
They are a development tool, not a way to install the provider. Use a
filesystem mirror for that.

### Tests

- `internal/client`: HTTP client tests against `httptest` servers with upstream
  response fixtures. They cover auth headers, error mapping, retries (read-only requests only,
  `Retry-After`), mutations never being retried, context cancellation,
  suffix resolution, update payloads, and toggle and delete behaviour.
- `internal/provider`: full plan/apply/import/refresh lifecycles run by
  OpenTofu (`TF_ACC_TERRAFORM_PATH`, set by `mise run test`) against an
  in-memory fake SimpleLogin API that reproduces upstream quirks.
- Live acceptance tests (`TestAcc*`) run only when **all** of these are set:

  ```bash
  export TF_ACC=1
  export SIMPLELOGIN_API_KEY=...               # key of a test account
  export SIMPLELOGIN_ACC_DOMAIN=test.example   # verified domain reserved for tests
  export SIMPLELOGIN_ACC_MAILBOX_EMAIL=me@example.net
  mise run testacc
  ```

  Each run creates `tfacc-<random>@$SIMPLELOGIN_ACC_DOMAIN`, updates it,
  imports it and deletes it. The deleted address stays in that domain's trash
  until you remove it there in the dashboard. Existing aliases are never
  touched. Normal CI never runs these tests.

### Releasing

1. Make sure CI on `main` is green and update `CHANGELOG.md`.
2. `git tag v0.1.0 && git push origin v0.1.0`
3. The release workflow runs the checks and tests, then GoReleaser creates a
   **draft** GitHub release with the zips, `SHA256SUMS` and the manifest.
   Review it and publish it.

Publishing to the Terraform or OpenTofu registry would additionally require
GPG-signed checksums and registry onboarding. That isn't set up yet.

## API notes and limitations

These behaviours were verified against the upstream SimpleLogin source
([simple-login/app](https://github.com/simple-login/app)) and drive the
implementation:

- **Creation** uses `GET /api/v5/alias/options` and `POST /api/v3/alias/custom/new`.
  For every verified custom domain, SimpleLogin offers a signed suffix
  `@<domain>`. The provider picks exactly that suffix, sends the local part
  as `alias_prefix`, and checks that the created address equals the requested
  one. Random-word suffixes are never used. Signed suffixes expire after about
  10 minutes, so they are fetched right before each create and never stored.
- **Address rules:** upstream lowercases the prefix and replaces characters
  outside `[a-z0-9._-]`, and allows at most 40 characters. The provider rejects
  such addresses at plan time instead of creating a different address.
- **Missing aliases:** `GET /api/aliases/:id` answers **400 "Unknown error"**
  (not 404) for an alias that doesn't exist, and `DELETE` answers 403. The
  provider removes an alias from state only after the alias search confirms
  that the ID no longer exists. 401, 403, 5xx and network errors always fail
  the run. They never drop the alias from state.
- **Enable/disable** uses the non-idempotent `POST /api/aliases/:id/toggle`.
  The provider reads the live state first, toggles at most once, verifies the
  returned state, and never retries the toggle.
- **Retries:** only read-only requests are retried: GET requests and the alias
  search (a read-only POST). They are retried on 429/502/503/504 and network
  errors, up to 3 times, honouring `Retry-After` up to 60s. Create, update,
  toggle and delete are never retried. If a create fails without a response,
  the error tells you to check the dashboard and import the alias if it exists.
- **Partial failures after create:** if the alias was created but disabling it
  or reading it back fails, the apply finishes with a **warning**, not an
  error. An error would make OpenTofu taint and replace the alias, which
  deletes it and blocks its address in the trash. The refresh before the next
  plan picks up the real values and plans an in-place update if needed.
- **Deletion:** custom-domain aliases go to the domain trash and block the
  address (see the warning above). According to the upstream source, removing
  the entry from the domain trash makes the address available again. This has
  not been verified against the hosted service. Aliases on SimpleLogin domains may instead
  be moved to the user trash, depending on account settings. Such an alias
  stays readable by ID (disabled) for a while.
- **Mailbox order:** the API returns an alias' mailboxes in arbitrary order,
  so `mailbox_ids` is a set.
- `GET /api/custom_domains` returns `{"custom_domains": [...]}` even though the
  API docs show a bare array. Both are accepted.
- Mailbox lookup uses `GET /api/v2/mailboxes`. It's implemented upstream but
  missing from the endpoint index of the API docs.

## License

[MPL-2.0](LICENSE)
