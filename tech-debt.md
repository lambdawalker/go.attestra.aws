# Technical debt: migration and compatibility register

This is the central register for the Attestra application, its three AWS features, and Infra Composer. It records transitional behavior and compatibility contracts that can otherwise be mistaken for dead code. Source audit only: no live state, stored policies, IAM roles, client versions, or operator machines were inspected. No entry is cleared for removal by this audit.

## Removal rules

- A successful deployment in one environment is not proof that all environments or consumers have migrated.
- Record evidence for every supported environment, standalone consumer, and rollback version before closing an entry. Record the cleanup PR and verification result here.
- Resource identity changes require previews against the relevant state; do not delete resources or remove state to make a preview pass.
- Persisted data and recovery compatibility may need to remain indefinitely. New code deployment alone does not migrate old records or backups.
- Owners below identify repositories, not assigned people. All entries are open; removal dates are intentionally unset until evidence exists.

## TD-01 — Email API route rename aliases

**Owner:** go.apexfission.aws.verifyemail. **Class:** removable migration bridge. **Status:** waiting for all-stack migration evidence.

[Source](https://github.com/lambdawalker/go.apexfission.aws.verifyemail/blob/08d3fb4eb9ea1f036d0e1c84a80974dae8015ba2/infrastructure/deployment.go) · [Migration guidance](https://github.com/lambdawalker/go.apexfission.aws.verifyemail/blob/08d3fb4eb9ea1f036d0e1c84a80974dae8015ba2/infrastructure/README.md)

The route registrations alias `route-signup`, `route-resend`, and `route-confirm` to `verifyemail-signup-route`, `verifyemail-resend-route`, and `verifyemail-confirm-route`. This adopts existing API Gateway routes after feature extraction instead of creating duplicate routes.

**Removal gate:** every supported old stack has adopted the new logical identities; record project/stack and unchanged physical route IDs. Preview a candidate without aliases in each affected environment and confirm no route create/delete/replacement. Include standalone consumers before publishing a cleanup version. Keep the route-alias tests until the supported upgrade window is formally closed. An old stack skipping the migration version would otherwise encounter the original duplicate-route problem.

## TD-02 — Root-level generated-state relocation

**Owner:** go.attestra.aws. **Class:** removable local-file migration bridge. **Status:** waiting for operator upgrade evidence.

[Source](https://github.com/lambdawalker/go.attestra.aws/blob/cf21fe2609f94ca07d4ef37d8085e6d1ad2d388f/tools/deploy/internal/workflow/generated_state.go) · [Tests](https://github.com/lambdawalker/go.attestra.aws/blob/cf21fe2609f94ca07d4ef37d8085e6d1ad2d388f/tools/deploy/internal/workflow/generated_state_test.go)

CLI startup moves old root-level bootstrap, teardown, publication receipts, recovery siblings, and teardown history into `.attestra/`. It refuses conflicting histories and active locks rather than overwriting progress.

**Removal gate:** all supported operator checkouts and recovery bundles use the new location, or a separate explicit migration utility remains available. Verify interrupted migration/recovery behavior and document the minimum upgrade path. Do not remove lock/conflict safeguards merely because routine deployments use the new directory.

## TD-03 — Unqualified dev bootstrap checkpoint

**Owner:** go.attestra.aws. **Class:** legacy reader and recovery cleanup. **Status:** waiting for checkpoint migration evidence.

[Setup reader](https://github.com/lambdawalker/go.attestra.aws/blob/cf21fe2609f94ca07d4ef37d8085e6d1ad2d388f/tools/deploy/internal/workflow/bootstrap_checkpoint.go) · [Deployment reader](https://github.com/lambdawalker/go.attestra.aws/blob/cf21fe2609f94ca07d4ef37d8085e6d1ad2d388f/tools/deploy/internal/workflow/deploy_settings.go) · [Teardown cleanup](https://github.com/lambdawalker/go.attestra.aws/blob/cf21fe2609f94ca07d4ef37d8085e6d1ad2d388f/tools/deploy/internal/workflow/teardown_local.go)

For dev only, missing environment-specific checkpoints can fall back to `.attestra/bootstrap.local.json`. Teardown also removes that old checkpoint and its backup so stale setup state cannot reappear.

**Removal gate:** inventory and migrate old dev checkpoints and recovery backups across supported operator machines; keep repository/environment validation. Retire the readers and legacy cleanup together only after old checkpoints cannot be reintroduced. Preserve current-format backup/recovery behavior.

## TD-04 — Legacy DNS provider inference

**Owner:** go.attestra.aws. **Class:** saved-settings compatibility. **Status:** waiting for settings normalization.

[Source and precedence](https://github.com/lambdawalker/go.attestra.aws/blob/cf21fe2609f94ca07d4ef37d8085e6d1ad2d388f/tools/deploy/internal/workflow/dns_provider.go) · [Tests](https://github.com/lambdawalker/go.attestra.aws/blob/cf21fe2609f94ca07d4ef37d8085e6d1ad2d388f/tools/deploy/internal/workflow/dns_provider_test.go)

Old wizard memory has a Route 53 selection but no explicit DNS provider. A known empty zone means Cloudflare; a nonempty zone means Route 53. Existing Pulumi zone configuration takes precedence. The wizard now saves provider and zone together.

**Removal gate:** all supported remembered settings explicitly store the provider, including restored checkpoints. Verify both providers and existing Pulumi configuration precedence. Removing inference early can unexpectedly reprompt or change operator selections.

## TD-05 — Older onboarding policy normalization

**Owners:** go.apexfission.aws.auth and go.appexfission.aws.card-capture. **Class:** persisted contract compatibility; coordinated cleanup required. **Status:** retain pending data/client and rollback audit.

[Auth normalization](https://github.com/lambdawalker/go.apexfission.aws.auth/blob/547a7af5b7564c2c4de44242b82c09b890655f43/runtimeconfig/onboarding.go) · [Auth JSON boundary](https://github.com/lambdawalker/go.apexfission.aws.auth/blob/547a7af5b7564c2c4de44242b82c09b890655f43/runtimeconfig/json.go) · [Capture normalization](https://github.com/lambdawalker/go.appexfission.aws.card-capture/blob/37fefc0b7e350c47bcba1d46c8e268960d3bcbdf/runtimepolicy/validation.go) · [Capture reader](https://github.com/lambdawalker/go.appexfission.aws.card-capture/blob/37fefc0b7e350c47bcba1d46c8e268960d3bcbdf/runtimepolicy/store.go)

Both features interpret policies without an onboarding block using the old features flag: ID capture is optional when enabled, disabled otherwise; the legacy default policy supplies the other requirements. Auth distinguishes omitted onboarding from explicit JSON null, which is invalid.

**Removal gate:** audit current policies, revision history, rollback documents, seeds, standalone users, and all supported producers. Define and perform an explicit data/contract migration, then test both features against identical fixtures. Deploying newer binaries does not rewrite every historical policy. Never remove explicit-null rejection as part of cleanup or silently strengthen enrollment requirements for old documents.

## TD-06 — Preserved authentication table and capture key identities

**Owners:** go.apexfission.aws.auth and go.appexfission.aws.card-capture. **Class:** stable persisted identities, not an automatic deletion task. **Status:** retain unless an explicit identity/schema migration is approved.

[Auth table registration](https://github.com/lambdawalker/go.apexfission.aws.auth/blob/547a7af5b7564c2c4de44242b82c09b890655f43/infrastructure/main.go) · [Capture fact reader](https://github.com/lambdawalker/go.apexfission.aws.auth/blob/547a7af5b7564c2c4de44242b82c09b890655f43/signin/capture_facts.go) · [Application migration boundary](docs/migration.md)

The auth grant/enrollment table still uses the historical logical name `email-proofs` after email verification moved to its own `verification-proofs` table. Auth's extracted capture fact reader preserves the existing `ACCOUNT#<owner hash>` and `CAP#<owner hash>#<id>` key contract with owner checks.

**Change gate:** decide whether renaming has enough value to justify migration risk. A logical table rename needs identity-preserving Pulumi aliases/adoption and previews showing no replacement. Key changes need coordinated readers/writers, data migration and rollback tests; they cannot be removed as unused fallback branches. Preserve owner validation. Keeping these stable names is a valid resolution.

## TD-07 — Capture replay compatibility for missing disclosure language

**Owner:** go.appexfission.aws.card-capture. **Class:** persisted-record/idempotency compatibility. **Status:** retain pending record-retention audit.

[Source](https://github.com/lambdawalker/go.appexfission.aws.card-capture/blob/37fefc0b7e350c47bcba1d46c8e268960d3bcbdf/capture/service.go)

Create-intent replay checks disclosure language when the existing record has one, but tolerates its absence. Replay occurs before current admission checks, preserving an existing intent when policy changes.

**Removal gate:** determine whether records and idempotency references without disclosure language can still be replayed, including recovery backups and rollback releases. Define expiry or migration evidence and test old/new intent replay. Do not invent historical disclosure evidence or remove replay-before-admission behavior as incidental cleanup.

## TD-08 — Composer description API compatibility

**Owner:** go.infra.composer. **Class:** public Go API compatibility. **Status:** retain until coordinated API transition.

[Source](https://github.com/lambdawalker/go.infra.composer/blob/ad4cfd268616a1ef94b324fe88266f845294f57a/ownership/metadata.go)

`Metadata.Description(_ string, limit int)` retains the unused purpose argument for callers from the former description format. An omitted Owner also falls back to Feature. Neither creates additional AWS resources.

**Removal gate:** audit supported app, feature and external callers, including older pinned module versions. Introduce/deprecate an API deliberately and update consumers before removing the argument. Requiring Owner is a separate behavior change: identify callers relying on its fallback first. Verify description format and length limits across adapters. Prefer keeping a cheap compatibility shim over an accidental breaking release.

## Compatibility that is not scheduled for removal

- **GitHub OIDC subject formats:** [setup](https://github.com/lambdawalker/go.attestra.aws/blob/cf21fe2609f94ca07d4ef37d8085e6d1ad2d388f/tools/deploy/internal/workflow/aws_setup.go) and [teardown ownership validation](https://github.com/lambdawalker/go.attestra.aws/blob/cf21fe2609f94ca07d4ef37d8085e6d1ad2d388f/tools/deploy/internal/workflow/teardown_cleanup.go) support repository/environment subjects with and without immutable IDs. These represent repository configuration, not merely one completed AWS deployment. Retire a format only after verifying supported repositories' claims, role tags/trust, setup and teardown behavior. Keep exact ownership/trust validation; do not broaden matching to simplify cleanup.
- **Explicit backend migration:** `--migrate-from` remains an operator feature described in [migration.md](docs/migration.md). One operator finishing migration does not establish that other users no longer need it.
- **Legacy teardown evidence guards:** refusing to certify old checkpoints without a pre-destroy inventory is a safety boundary. Do not remove it to eliminate a legacy error.
- Reporting's ARN/provider-ID lookup aliases and CloudFront/Route 53 DNS aliases are current identity resolution/routing mechanisms, not the Pulumi rename bridge in TD-01.

## Audit baseline and follow-up

Reviewed default-branch snapshots:

- [go.attestra.aws](https://github.com/lambdawalker/go.attestra.aws/tree/cf21fe2609f94ca07d4ef37d8085e6d1ad2d388f): `cf21fe2609f94ca07d4ef37d8085e6d1ad2d388f`
- [go.apexfission.aws.auth](https://github.com/lambdawalker/go.apexfission.aws.auth/tree/547a7af5b7564c2c4de44242b82c09b890655f43): `547a7af5b7564c2c4de44242b82c09b890655f43`
- [go.apexfission.aws.verifyemail](https://github.com/lambdawalker/go.apexfission.aws.verifyemail/tree/08d3fb4eb9ea1f036d0e1c84a80974dae8015ba2): `08d3fb4eb9ea1f036d0e1c84a80974dae8015ba2`
- [go.appexfission.aws.card-capture](https://github.com/lambdawalker/go.appexfission.aws.card-capture/tree/37fefc0b7e350c47bcba1d46c8e268960d3bcbdf): `37fefc0b7e350c47bcba1d46c8e268960d3bcbdf`
- [go.infra.composer](https://github.com/lambdawalker/go.infra.composer/tree/ad4cfd268616a1ef94b324fe88266f845294f57a): `ad4cfd268616a1ef94b324fe88266f845294f57a`

The separate, unfinished environment-admin migration branch is outside this main-branch audit. Add any temporary shared-admin bridge it introduces when that work is integrated. This register is a focused migration/compatibility audit, not a claim that all technical debt has been discovered.

For each cleanup, append: affected environments/consumers, old-state or old-data evidence, rollback window decision, proposed removal PR, validation/preview results, and final status. Record metadata only; never commit credentials, raw state exports, private reports, or user records.
