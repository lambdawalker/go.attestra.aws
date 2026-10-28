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
- An authenticated AWS profile/session or CI OIDC role. Authenticate with your
  existing AWS CLI login/SSO flow before running the wizard.
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

Choose shared, backend, or both. Both deploys shared first and validates its
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

CI uses the same wizard with explicit flags and a protected GitHub environment.
Set WIZARD_SETTINGS_JSON to the reviewed contents of local .attestra/wizard.json
(nonsecret choices only). CI requires this snapshot so capture capacity, Android
associations and DNS choices are not silently replaced by first-run defaults.
The workflow clears the local AWS profile choice and uses its OIDC session.
Configure `DEPLOY_ROLE_ARN`, `AWS_ACCOUNT_ID`, `AWS_REGION`,
`PULUMI_BACKEND_URL`, `BASE_DOMAIN`, `SENDER_DOMAIN`,
`SENDER_ADDRESS`, `ADMIN_EMAIL` as environment variables and
`PULUMI_CONFIG_PASSPHRASE`, `CLOUDFLARE_API_TOKEN` as secrets.
OIDC trust must authorize this repository's selected GitHub environment.
A role authorized for the legacy repository does not automatically authorize
this repository.

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

Terminal prompts hide the Pulumi passphrase and Cloudflare token. They remain
in memory for the run; only nonsecret choices are saved. For unattended use,
supply the documented environment variables. Save credentials separately in
your password manager; the legacy vault is neither copied nor modified.

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
