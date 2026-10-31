# Parallel deployments

The launchers run `go run -buildvcs=false ./cmd/deploy` from the repository root and preserve
arguments and exit status. The library owns wizard logic and infrastructure;
this repository owns entrypoints and build targets.

## Deployment identities

The initial prefix is `next`; explicitly select empty for final domains.
A literal prefix `final` is reserved. Prefixes are lowercase alphanumeric,
start with a letter, and have at most 20 characters. Logical environment names
are lowercase DNS-safe names; the combined stack name must fit 32 characters.
Changing prefix selects another deployment, never renames existing resources.

For base domain `attestra.com` and environment `dev`:

| Purpose | Test deployment | Final deployment |
| --- | --- | --- |
| App links / passkey domain | next.dev.attestra.com | dev.attestra.com |
| Backend API | next.api.dev.attestra.com | api.dev.attestra.com |
| Index | next.index.attestra.com | index.attestra.com |
| Administrator | next.admin.attestra.com | admin.attestra.com |

All domain names above describe the new deployment convention. Compare the final
hostnames with the actual legacy stack outputs before cutover; do not assume
legacy API and app-link domains are interchangeable.

The backend's registry environment identifier is its full stack name, for
example `next-dev`. Administrator grants and runtime settings use that same
identifier. Shared infrastructure is per deployment prefix, not per environment.

## Prerequisites

- Go 1.26.6, Pulumi CLI and AWS CLI v2.
- An AWS IAM Identity Center (SSO) profile for local runs or an OIDC role for CI.
  The wizard checks SSO and streams login instructions when an interactive
  session has expired. Noninteractive runs never start SSO login.
- The expected AWS account and region.
- An existing private, versioned S3 state bucket with public access blocked.
  Select a dedicated backend base; the wizard adds deployment isolation.
- An existing verified SES domain identity in the selected account/region and a
  matching sender address. The sender domain is never prefixed.
- Cloudflare DNS access for the domain, or a supported Route53/preissued
  certificate configuration as described by the wizard help.
- A Pulumi configuration passphrase. Keep it in a password manager.

The legacy credential vault is not automatically migrated. Do not rename its
encrypted file: the original scope includes repository/environment identity.

## Run

```sh
./deploy.sh
# The same command on Windows:
# deploy.bat

# List supported noninteractive inputs:
go run -buildvcs=false ./cmd/deploy --help

# Build artifacts only, without AWS:
go run ./cmd/attestra build
```

Choose Admin/index (`admin-index`/`shared`), Backend (`backend`), or All (`all`/`both`). Both deploys shared first and validates its
account/region/stack outputs before configuring the backend. Choose preview to
inspect an already initialized target; preview does not initialize stacks,
publish DNS, grant administrators or publish registry changes. First deployment
uses explicit deploy initialization. Existing stack configuration is restored
locally in encrypted form before use. Preview rejects changed resource settings
rather than showing a diff for stale configuration; choose deploy to review and
apply those settings. Code-only changes can still be previewed.

Inputs and immutable deployment bindings are stored privately in ignored
`.attestra/`. Keep recovery receipts when an operation is interrupted.
Do not edit a receipt to bypass a mismatch or lock.

## Configure deployment from GitHub

Merge the reviewed workflow changes before dispatching them from GitHub Actions.
Authenticate your GitHub CLI once with `gh auth login`, and use AWS SSO locally.
The GitHub account needs repository environment/variable/secret administration
permissions. The wizard asks **Set up deployment from GitHub? [Y/n]** and handles
the rest: it generates separate admin/index and backend AWS deployment policies,
shows their complete JSON for explicit approval, creates/reuses OIDC roles and
runtime permission boundaries, and reconciles GitHub environments, variables and
encrypted secrets. No hand-written policy file or role ARN is needed.

To configure a selected target without deploying it:

```powershell
.\deploy.bat --operation setup-github --scope all --environment dev --prefix next
```

Generated JSON is saved under `.attestra/github-policies/`. Permission approval
has no timer; read the scope notes and answer `yes` to approve. Subsequent runs
reuse the approval when permissions match, while still checking remote policies,
role trust and environment settings for drift. Permission changes require another
review. For unattended enrollment, first generate/review the files, then rerun
with `--yes --github-approve-policy <review-SHA256>`. Ordinary Actions use the
saved runtime pins and do not enroll IAM resources.

Policies restrict state objects to this bucket/identity prefix, and Lambda code
and runtime roles to target-specific names with mandatory execution boundaries.
Some AWS creation/discovery APIs require broader regional permissions; existing
table/bucket/queue families overlap modular environments, and optional CloudFront
permissions are global within the account. The displayed documents show these
limits. State KMS keys are discovered by exact ARN, with cryptographic access
restricted through S3; existing KMS key policies must allow IAM use. The state
bucket name cannot overlap `id-evidence-*` or `app-associations-*` application
families in automatic mode. No AdministratorAccess or permanent AWS keys are used.

For an existing stack, automatic runtime names can cause role/function
replacements. Complete a **local SSO deployment** and inspect the Pulumi preview
before dispatching Actions. If setup detects an existing reset function without
the new boundary, it creates the approved boundary and asks for local adoption:

```powershell
.\deploy.bat --operation deploy --github-setup=false
.\deploy.bat --operation setup-github
```

This preserves saved answers and the encrypted vault. After local adoption,
retrying setup finishes GitHub configuration. Old immutable policies remain until
you separately verify they are unused and remove them.

Advanced overrides are available: `--github-policy D:\secure\reviewed-policy.json`
or existing `--github-shared-role-arn` / `--github-backend-role-arn` values select
manual enrollment. Existing manual deployments keep their runtime naming unless
automatic mode is explicitly adopted.

The wizard configures `next-shared` and `next-dev` environments; choose
`--scope admin-index` or `--scope backend` to configure only one. Existing branch
protections and unrelated settings remain intact. Incompatible trust, protections,
unexpected permissions on automatic roles, or changed immutable policy contents
stop for manual review. Setup never dispatches an Action.

Open **Actions → Deploy → Run workflow**. Select Admin/index, Backend or All, the
logical backend environment, prefix and operation. All deploys shared once and
then the selected backend through its own protected environment. It does not
implicitly deploy every environment. CI uses OIDC and explicit approval, never
SSO or the interactive countdown. No deployment is dispatched by setup itself.

Setup fingerprints public configuration and checks actual variables, main-only
branch restrictions, OIDC trust and secret inventory before skipping writes.
Encrypted local delivery metadata detects changed local secrets; only an opaque
rotation identifier contributes to the public fingerprint. Use
`--github-force-reconcile`, `--github-rotate-secrets` or a new
`--github-secret-version` for deliberate redelivery/recovery. Without a vault key,
setup conservatively redelivers secrets. Keep the vault out of CI artifacts.


## Shared email ownership

The new backend references the existing SES identity. It does not declare or
delete the SES identity, DKIM, verification TXT records or MAIL FROM records.
Only app/API/admin/index domains are prefixed. Email verification links return
to the new app origin even though the sender address stays unchanged.

Legacy still owns its SES resources until a deliberate ownership transfer.
Before any legacy teardown, preserve the identity and its DNS records under a
surviving owner. Do not simply destroy the original stack and assume mail will
continue working. That ownership/teardown operation is not performed by this
wizard.

## Android and cutover

The public Android configuration is written to
`.attestra/<full-stack>-android.properties`, for example
`.attestra/next-dev-android.properties`. Supply that file to your Android build
as its public configuration input. Configure
the corresponding app-link/passkey domain association using the wizard prompt
or `--android-associations` JSON. Each entry contains `packageName` and
`sha256CertFingerprints` (an array of actual signing-certificate SHA-256 fingerprints).
This provisions association hosting for the Android package
and signing certificate before testing passkeys. Pool/client IDs and app/index
URLs must all refer to the same deployment.

Validate email link and manual-code confirmation, sign-in/refresh, passkey
registration/login, configured password enrollment, capture/upload/processing,
admin invitation/MFA, runtime setting changes and environment discovery.

After the prefixed deployment passes live checks, preserve email resources and
any data/settings you want to retain. Retire legacy only with a separately
reviewed teardown plan. Deploy the final identity with an explicit empty prefix
while keeping the test deployment available. Reconfigure Android for the final
deployment and rerun live checks before retiring the prefixed deployment.

Both fresh deployments have different Cognito users and passkeys. Test users
and their credentials are disposable; data is not migrated automatically.
This change does not add an automatic destructive teardown operation.

## Credentials and interrupted operations

The first interactive deployment creates a private encrypted credential vault
using the existing AES-GCM/Argon2id implementation. Its scope is this checkout
and deployment prefix, so environments sharing the same admin/index stack reuse
the shared Pulumi passphrase. The vault unlock passphrase is separate from the
Pulumi passphrase and is never stored. Secret input is hidden; nonsecret settings
remain in ignored `.attestra/wizard.json`. Existing settings migrate without
changing the Version1 CI representation. The legacy credential vault is not copied.

On reruns, select the target and operation; valid saved answers are skipped.
Use `--reconfigure` to revisit settings and `--replace-cloudflare-token` to replace
the encrypted DNS token. `--operation rotate-secrets` rotates only the vault
unlock passphrase, not Pulumi's existing state encryption key. Back up the vault
and retain both passphrases in your password manager. A fresh checkout restores
Pulumi's authoritative encrypted configuration before updating it.

After a successful preview, local deployment applies the saved Pulumi plan.
The visible five-second countdown proceeds by default; Enter/yes proceeds
immediately, and no/Escape/Ctrl+C/EOF cancels. Destructive previews require a
separate explicit phrase without a timeout. Unattended runs require `--yes`.
S3 state access uses IAM; its bucket encryption is distinct from Pulumi secret
encryption and the local vault.

A failed registry publication has a durable receipt in the selected state bucket.
Use the same deployment target with `--operation publish` to resume publication
without another infrastructure update. Stale identity locks require inspection
of active operations and pending receipts before manual recovery. Do not delete
locks merely because a previous terminal was closed.

Cloudflare-created ACM certificates are owned by wizard receipts outside the
Pulumi stacks and are deliberately retained. Any later teardown plan must include
those certificate receipts and DNS records. Preissued certificate references and
shared email resources are externally owned and must not be blindly deleted.

The library's [wizard reference](https://github.com/lambdawalker/go.apexfission.aws.auth/tree/feat/parallel-deployment-wizard/deploy/wizard)
documents all flags, ownership boundaries, recovery, and health-check limits.

## Reuse settings from the original repository

In `go.attestra.aws.auth`, run `./export-wizard.sh --environment dev` or
`export-wizard.bat --environment dev` on Windows. Repeat for other environments.
This writes `~/.attestra/legacy-wizard.json` with reusable nonsecret settings.
Then launch this repository's `deploy.sh` or `deploy.bat` normally. After you
choose the environment, questions covered by the export are skipped, including
the existing SES sender domain/address. Missing values still prompt. Command-line
flags and locally saved wizard choices take priority. Prefix, scope and operation
are still selected normally. The export does not transfer legacy resource
ownership, proof keys, certificates or credential vaults.

## Capture recovery update

The backend now includes the original repository's manual-review and stalled
extraction recovery fixes through commit `4607807`. Ready captures whose
extraction failed can save manually entered data. Abandoned extraction becomes a
terminal failure after its recovery window, without interrupting a live worker.
The capture worker also gains account-record UpdateItem permission for the
atomic readiness transaction. Deploy the backend infrastructure as well as its
Lambda code to apply this permission; rebuilding the app alone is insufficient.

After deploying, test failed-extraction manual entry and saving, a normal
extraction, and account readiness. The form should finish only after persisted
`reviewed` data is returned or reconciled. Diagnostics avoid logging card fields
and inference response contents.

## Build reuse and parallel work

`--operation build` builds the selected scope without AWS credentials or state.
The ignored `.lambda-cache/` stores content-addressed ZIPs and receipts. Actual
source dependencies, embedded files, Go module/workspace/replacement inputs,
toolchain and build settings are fingerprinted; restored artifacts are verified.
Identical signin entrypoints compile once. Independent packages build with a
bounded worker pool; `--build-jobs N` changes its limit. `--force-rebuild` and
`--clean-cache` apply only to the current invocation. Inspect an abandoned
`.lambda-cache.lock/` before manually removing it; never remove an active lock.

Pulumi receives the base64 SHA-256 of the actual Lambda ZIP bytes. Unchanged code
can avoid compilation/upload while IAM and function configuration still receive
a normal plan. Hashes do not replace remote-state checks or operation locking.

## Failed reviewed deployments

Approval checks the saved plan's resource operations. Words such as
DeleteFunctionConcurrency inside an IAM policy do not classify an update as
destructive. Actual deletes/replacements still require the explicit phrase;
unknown plan operations or formats stop before apply. The unwrapped format is
recognized from its resourcePlans object; manifest.version is CLI build metadata.
Labels such as v3.273.0 or development builds do not invalidate a structurally
compatible plan. Real CLI-generated update/delete fixtures cover this behavior.

Failed Pulumi preview/up commands now show redacted diagnostics. Config/export
and secret-writing output stays suppressed. Keep the diagnostic error when
reporting a failure; an exit status alone cannot identify the AWS/Pulumi cause.

Runtime defaults use deterministic Pulumi outputs. The shared index records the
actual timestamp during fenced publication, preserving it when defaults do not
change. When upgrading this fix, deploy **All** once so the index and backend use
the same publication behavior:

```powershell
.\deploy.bat --operation deploy --scope all --environment dev --prefix next
```

After a failed apply, rerun the wizard to refresh current state and review a new
plan; some planned operations may already have completed. Keep the existing state.
The printed plan path is temporary and is removed when the invocation ends.

## AWS startup diagnostics

An AWS CLI exit status such as 255 alone does not identify the failing operation
or cause. The wizard includes service/operation and redacted stderr. AWS stdout
is withheld on failure because credential exports and object reads can contain
secrets; debug-mode output stays suppressed.

Interactive renewal supports both explicit profiles and AWS's ambient/default
SSO selection. It requires a recognized expired/invalid session and confirmed SSO
configuration, then checks the account again after login. It does not retry
permission/network errors as login problems or start interactive login in CI.

Check credentials independently with the same profile the wizard uses:

```powershell
aws sts get-caller-identity --region us-east-2
```

Add `--profile NAME` if you selected a named profile. If the CLI reports SSO expiry,
renew with `aws sso login` using that same profile. If STS succeeds, investigate
the operation named in the wizard's diagnostic rather than assuming the failure
is authentication. Set a named wizard profile with `--profile NAME` when needed.
