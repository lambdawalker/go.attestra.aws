# Attestra environment tools

This Go module contains the setup, deployment, credential management, health-check, Android export, and teardown commands. Run them through the repository's `.bat` / `.sh` launchers or the existing entry point:

```sh
go -C tools/deploy run . -bootstrap
go -C tools/deploy run . -teardown
go -C tools/deploy run . -teardown-index
go -C tools/deploy run . -teardown -nuke
go -C tools/deploy run . -teardown -nuke -check
go -C tools/deploy run . -teardown -nuke -fast-check
go -C tools/deploy run . -manage-credentials
```

The launchers supply the repository path explicitly. Direct commands retain the existing `-repo-root` option and default. `main.go` only calls the workflow entry point and handles process exit. GitHub Actions uses the same executable.

## All-environment nuke

From the repository root use `deploy.bat -teardown -nuke` or `./deploy.sh -teardown -nuke`. This delegates orchestration to Composer's `teardown.Engine`; the application adapter supplies account-bound environment discovery and the cleanup that lives outside Pulumi. No environment target picker is used. An optional vault prompt selects credentials only, not deletion scope. Administrator login and typed repository/account confirmation are required before destruction.

Discovery combines GitHub environment variables, local setup/teardown records, canonical ownership tags on account-owned S3 state buckets, and the saved Composer report. Every discovered `attestra-application` environment is attempted before shared `attestra-control-plane` admin/index stacks. Missing artifacts count as success; malformed/inaccessible discovery evidence does not authorize an incomplete scope. Distinct project/backend passphrases are requested when needed and stay in memory. The feature runtime itself owns no application-wide teardown flow.

The app adapter also captures exact Cloudflare DNS records, immutable repository-bound deployment roles, GitHub environments and the known bootstrap marker keys. These identifiers are persisted before deletion. Active/foreign bootstrap locks, unknown historical marker payloads, ambiguous DNS, shared roles and unverifiable ownership remain protected. The AWS caller's own deployment role is never deleted. Marker history cleanup is limited to exact validated app-owned keys under the existing conditional bootstrap lease; generic Composer state/recovery checks remain in force.

State buckets and KMS keys are processed only after earlier cleanup is verified. KMS use does not establish exclusive ownership: the operator must independently confirm each customer-managed key's exclusivity before it can be scheduled for deletion. Unapproved keys retain recovery data. Secrets Manager scheduled deletion and retained SYSTEM/AWS Backup data remain present until actual absence can be verified; this may keep a nuke incomplete after its stack has been removed. No secret values are read or saved in the report.

The private `.attestra/nuke.<account>.local.json` report shows status, artifact type, zone/region, environment and identifiers. It is Composer's report format, not an automatic import of the legacy auth repository's nuke receipt. Rerun nuke to retry cleanup with its surviving inventory. Credential vaults and local deployment/setup records remain private and are retained; a nuke is not a source-code or credential wipe.

Use `-teardown -nuke -check` for read-only verification of the saved inventory. `-teardown -nuke -fast-check` requires a previous check and queries only the latest `REMAINS` entries. Skipped evidence keeps its timestamp, and skipped unknowns do not become success. Checks do not discover new environment targets or perform deletion; resource-scoped DNS zone lookup can recover missing zone evidence after a token is supplied. Reports containing only external artifacts can still be checked after stacks disappear.

Keep all deployments suspended during nuke. A local report lock blocks other operator flows in this checkout; active GitHub deployments are checked before destructive phases, and marker cleanup uses the shared bootstrap lease. These guards are not an account-wide distributed nuke lock across other checkouts or tools. Other deployments must remain stopped until cleanup is finished.

## Package map

All paths below are relative to this directory.

| Directory | Responsibility |
| --- | --- |
| `internal/workflow` | CLI dispatch, setup/deploy/teardown sequencing, checkpoints, AWS login renewal, capacity policy, health checks, and deployment summaries |
| `internal/aws` | AWS CLI transport, redacted errors, and subprocess environment filtering |
| `internal/build` | Parallel Lambda compilation, archive packaging, and archive checks |
| `internal/dns` | Cloudflare API access, DNS planning, ownership comparisons, and approved record deletion |
| `internal/github` | GitHub environments, encrypted secret submission, and deployment/sharing checks |
| `internal/vault` | Credential encryption, file format, passphrase validation/rotation, and key-buffer cleanup |
| `internal/registry` | Signed environment-index requests, credential-provider refresh, and request retries |
| `internal/android` | Public Android properties-file serialization |
| `internal/ui` | Shared terminal prompts, environment picker, and environment-name validation |

Workflow code composes these packages. Reusable packages must not import `workflow`; supply data or a narrow callback/interface instead. For example, AWS CLI transport accepts a renewal-error callback, while the workflow owns interactive authentication. Route53 configuration remains in workflow because it is applied through the Pulumi deployment stages. Index infrastructure setup and deployment receipts likewise remain with their workflows.

Keep unit tests beside their implementations. Tests that exercise several packages together belong in `internal/workflow`. Launcher tests stay at the module root so they continue testing the real repository launchers.

## Verification

From the repository root:

```sh
go -C tools/deploy test -race ./...
go -C tools/deploy vet ./...
go -C tools/deploy build -o attestra-deploy ./
```

Use `./...` when testing: the command package alone does not include the internal package tests. Tests use local fixtures and fake services; they do not deploy or tear down cloud environments.

Existing deployment flags, checkpoint JSON, vault encryption/location, Pulumi configuration, Android export keys and the GitHub workflow entry point retain their behavior. The all-environment nuke flags are described above. See [migration and deployment](../../docs/migration.md) for the application project/state boundaries.

Windows credential renewal uses an unquoted absolute helper path because the AWS Go SDK's `cmd.exe` invocation does not preserve quoted executable paths. Paths containing spaces use the Windows short-path alias. If that filesystem has short names disabled, the wizard stops with instructions to set `TEMP` and `TMP` to an existing path without spaces (for example, `C:\attestra-temp`) and rerun. If `GOTMPDIR` is set, update it too; for a prebuilt executable, move it to such a directory. It never changes the original AWS profile.

## Generated local state

Wizard progress files live in ignored `.attestra/` at the repository root:

- `bootstrap.<environment>.local.json`: non-secret setup choices, progress, health/completion receipts and configuration fingerprints.
- `teardown.<environment>.local.json`: teardown progress, resource inventory and deletion verification.
- `index-publication.<environment>.local.json`: pending publication/recovery receipts.
- `teardown-history/`: archived completed teardown records and their encrypted configuration backups.

Their `.bak`, `.tmp` and `.lock` siblings stay alongside them. Stop any running setup/deployment/teardown before upgrading. On the next command, old root-level files are moved intact. Conflicting old/new files or existing operation locks stop migration for inspection; no history is overwritten. Interrupted moves resume on rerun.

Keep `.attestra/` when resuming interrupted operations. These files contain no credential values but include infrastructure identifiers and recovery metadata; keep them private. The optional encrypted credential vault stays in its existing user-specific location. Build artifacts already live in `dist/`, public Android exports in `android-config/`, and Pulumi configuration stays in `infra/` and `infra-index/`.

Shared-index teardown uses `-teardown-index` and its private `.attestra/teardown-index.<account>.<region>.local.json` checkpoint. It does not replace environment teardown. See [migration and deployment](../../docs/migration.md) for the current project/state names.

## Export values for the modular deployment wizard

From the repository root, run `./export-wizard.sh --environment dev`, or on
Windows `export-wizard.bat --environment dev`. Requires Python 3 and PyYAML
(`python -m pip install -r scripts/requirements-export.txt`).
It reads local `infra/Pulumi.<environment>.yaml` and
`infra-index/Pulumi.shared.yaml` without contacting a state backend or decrypting
secrets, plus `.attestra/deploy.<environment>.local.json` when available.
If a local stack file is absent, it falls back to read-only
`pulumi config --json --non-interactive` using the current Pulumi backend/session.
A failed fallback reports Pulumi stderr and recovery guidance instead of a traceback. Run once per environment; exports merge into
`~/.attestra/legacy-wizard.json` with private file permissions.

Exports include sender domain/address, AWS region/account when available,
base domain derived from the index domain, Android associations, environment
class, and saved profile/state base. The new wizard automatically skips questions
for these values. Missing settings still prompt. Flags and local wizard settings
win over exports. Legacy domains, certificate ownership, secrets and credential
vaults are excluded. This does not transfer infrastructure ownership or deploy.
Use `--output PATH` only to inspect/export elsewhere; the new wizard discovers
the default home-directory path automatically.

## Application composition

This operator belongs to `github.com/lambdawalker/go.attestra.aws`.
`internal/build` reads the authentication feature's `artifacts.Lambdas()` manifest,
builds each unique full Go import path once from the application's pinned module
graph, and packages its archives alongside the application-owned `./cmd/admin`.
The shared workflow separately builds local `./cmd/index` and `./cmd/admin`.
Both `scripts/build.sh` and `scripts/build.bat` invoke this same Go builder.
A sibling authentication checkout is not required. Commit dependency changes in
`go.mod` and `go.sum`; they participate in the shared infrastructure source hash.

The new Pulumi project identities are `attestra-application` and
`attestra-control-plane`. Existing legacy projects are not selected or migrated
automatically. An explicit state migration needs separate review before use.

Teardown inventory excludes resources explicitly exported as non-custom Pulumi
components; exports lacking that metadata retain their AWS probes. Authentication
proof secrets are checked with Secrets Manager metadata only, including provider
`ARN|VersionId` version identifiers. No secret value is fetched. A secret still
inside AWS's scheduled deletion/recovery process is reported as `RETAINED` with
its deletion deadline and an explicit statement that absence is not yet verified.
Its versions share that retained parent state. This is a recoverable exception,
not a `DELETED` result: teardown preserves the default recovery window and never
forces deletion. Active secrets/versions are `REMAINS`; access, malformed-response
and transport failures are `FAILED` and require investigation. Saved validation
reports preserve the pending deadline for a later metadata-only follow-up.

## Timed deployment confirmations

Use `deploy.bat -timed-confirm` (Windows) or `./deploy.sh -timed-confirm` to give routine deployment approvals a five-second deadline. `setup.bat -timed-confirm` / `./setup.sh -timed-confirm` and `-scope shared` / `-scope both` support the same mode. Without the flag, confirmations wait for an answer as usual.

The prompt identifies the operation and states that it will continue automatically. Choose Continue to proceed immediately, or Cancel / Ctrl+C to stop. On timeout, the tool reports that it is continuing. Pulumi's final apply prompt is replaced with this timed approval; after approval Pulumi receives `--yes --non-interactive`. Preview and errors still run normally; an error or interruption is never treated as approval.

This is an interactive deployment mode, not a way to supply missing configuration. Credentials, text input, menus, repository/OIDC choices, signing-identity approvals, and vault-saving/replacement prompts still require an answer. Timed approvals cannot be combined with teardown/nuke, state migration, lock recovery, credential management, signing-approval management, or CI mode. The flag applies only to the current invocation and is not saved as a preference.

The reusable policy comes from Composer’s `approval` package. Attestra supplies the five-second setting, operation classification, terminal rendering, CLI flag and Pulumi command integration; Composer controls deadline-versus-cancellation semantics and keeps protected operations manual.
