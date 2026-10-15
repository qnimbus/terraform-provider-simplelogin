# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

OpenTofu/Terraform provider for SimpleLogin (Terraform Plugin Framework, protocol 6). Canonical address: `registry.terraform.io/qnimbus/simplelogin`. Always write that full source in HCL: in OpenTofu the short `qnimbus/simplelogin` resolves to `registry.opentofu.org`, a different address.

## Commands

Tooling (Go, OpenTofu) is pinned in `.mise/config.toml`; tasks live there too. `make` is not used.

```bash
mise run build          # bin/terraform-provider-simplelogin
mise run check          # gofmt, tofu fmt -check, go mod tidy -diff, go vet
mise run test           # all tests; provider tests drive real OpenTofu, no credentials
mise run docs           # regenerate docs/ (tfplugindocs) — CI fails if docs/ is stale
mise run dev-validate   # tofu validate examples/dev + examples/infrastructure via dev_overrides
mise run dev-plan       # tofu plan examples/dev with the local build (no API calls)
mise exec aqua:golangci/golangci-lint@latest -- golangci-lint run ./...
```

Single test (provider tests need an OpenTofu binary; `mise run test` sets this automatically):

```bash
TF_ACC_TERRAFORM_PATH=$(mise which tofu) go test -count=1 -run TestAliasResourceLifecycle -v ./internal/provider/
go test -run TestGetAliasMissing -v ./internal/client/
```

`mise run testacc` runs **live** tests against SimpleLogin; they skip unless `TF_ACC=1`, `SIMPLELOGIN_API_KEY`, `SIMPLELOGIN_ACC_DOMAIN` and `SIMPLELOGIN_ACC_MAILBOX_EMAIL` are all set. Each run creates and deletes one `tfacc-*` alias, leaving it in the domain trash. Never run them without explicit user approval.

The race detector needs cgo; there is no gcc in the local WSL setup, so `-race` only runs in CI.

## Architecture

- `internal/client` — plain `net/http` SimpleLogin API client, no Terraform types. `do()` retries only GETs; `request(..., retryable)` exists so the read-only alias search (a POST) can be retried too. Mutations are never retried.
- `internal/provider` — framework resource/data sources. All state writes for `simplelogin_alias` go through `aliasModelFromAPI(alias, onDestroy)`; `on_destroy` is state-only (never sent to or returned by the API), so every write must carry it explicitly.
- `internal/provider/fakeserver_test.go` — in-memory fake SimpleLogin used by provider tests via `resource.UnitTest`. It deliberately reproduces upstream quirks; keep it faithful when adding behaviour, and use its fault knobs (`failGets`, `failNextGets`, `failToggles`) for failure-path tests.

## SimpleLogin API behaviour that shapes the code

Verified against the upstream source (github.com/simple-login/app), which is more reliable than its `docs/api.md`:

- `GET /api/aliases/:id` returns **400 "Unknown error"** for a missing alias (not 404); DELETE/PATCH/toggle return 403. `client.GetAlias` only reports `ErrNotFound` when the search listing confirms the ID is gone — never drop resources from state on 400/401/403/5xx alone.
- Creation: `GET /api/v5/alias/options` → pick the suffix exactly `@<domain>` (only offered for verified custom domains) → `POST /api/v3/alias/custom/new`. Signed suffixes expire after ~10 min; fetch right before create. Upstream lowercases/rewrites prefixes, so `client.SplitAliasEmail` rejects anything outside `[a-z0-9._-]{1,40}` at plan time.
- Deleting a custom-domain alias moves the address to the domain trash; re-creating it returns 409 until it's removed from the trash in the dashboard. Hence: errors after a successful create are downgraded to warnings (an error would taint → replace → delete the alias), and `on_destroy = "disable"` exists.
- `POST /api/aliases/:id/toggle` is not idempotent: read live state first, toggle at most once, verify, never retry.
- Alias mailboxes come back in arbitrary order → `mailbox_ids` is a set. `GET /api/custom_domains` returns `{"custom_domains": [...]}` (docs show a bare array). The API exposes no DNS record data.

## Releases

Pushing a `v*` tag runs `.github/workflows/release.yml`: tests, then GoReleaser creates a **draft** release (linux_amd64, linux_arm64, windows_amd64 zips + SHA256SUMS; no GPG signing). Consumers install via an OpenTofu filesystem mirror using `scripts/install-provider-mirror.sh`; `tofu init` cannot install from a GitHub URL. Update `CHANGELOG.md` before tagging.

## Conventions

- Work on a branch and open a PR; CI (`.github/workflows/ci.yml`) runs check, build, race tests, example validate/plan, lint, docs drift and a GoReleaser snapshot.
- After changing schemas or `examples/`, run `mise run docs` and commit `docs/`.
- Go files start with `// SPDX-License-Identifier: MPL-2.0`.
- Examples and docs use placeholder domains/addresses only; this repo must not reference the separate infrastructure repo's tooling (e.g. DNSControl).
