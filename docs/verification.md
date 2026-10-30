# Verification scope

The deployment wizard and both infrastructure entrypoints are checked locally;
no AWS update, DNS write, administrator invitation, email delivery or teardown
was executed during implementation.

Local verification includes root race tests and vet, compilation of backend and
shared Pulumi entrypoints, shell launcher argument/cwd/exit-status behavior,
Windows amd64 CLI cross-compilation, and actual packaging/inspection of all 22
Linux ARM64 Lambda archives. Library tests cover runtime enrollment policies,
admin authorization, prefix/provider/shared-output guards, external SES ownership,
read-only preview, encrypted config recovery in fresh checkouts, resumable
DNS/admin/export/publication, TLS/DNS planning and scoped deployment order.
The consumer was verified with GOWORK=off against published commit
`eda2f3f2a93633e3b005f761e0d94ee24a4cf15c`, with no local module replacements.
Launchers disable optional VCS stamping so exported source trees and constrained
Git environments can run them. The batch launcher was statically inspected;
the Windows executable was cross-compiled, not executed on Windows.

Pulumi mocks and fake AWS commands cannot establish live infrastructure readiness.
Run the prefixed deployment and verify email/manual confirmation, passkeys and
Android association hosting, password enrollment where enabled, capture processing,
admin MFA, registry publication and runtime settings before the final deployment.

Existing legacy stacks, their state, vaults, users and evidence remain untouched.
Preserving shared SES ownership before legacy teardown is a separate operation.

## Upstream capture synchronization

The 2026-10-30 port incorporates original backend changes through `4607807`.
Library runtime tests/vet, capture adapter race tests, and Pulumi backend tests/vet
passed. The consumer pins published framework commit
`8e74bfb7ed0eb42919cc7dc2efd1b5a1dac2904c` for runtime and infrastructure,
without local replacements. This port has no live AWS or device acceptance test.

## Deployment conventions update

New checks cover dependency/overlay/toolchain build-cache invalidation, artifact
corruption, duplicate package reuse, process-tree cancellation and Windows
cross-compilation. Operator tests cover scoped saved answers, encrypted-vault
behavior, SSO instruction streaming, saved-plan approval, a fake-clock countdown,
EOF/Escape/Ctrl+C and destructive-confirmation cancellation. GitHub setup tests
mock environment drift, secret inventory/rotation, marker-last failure recovery
and exact repository/environment OIDC trust. Workflow routing tests execute the
actual input-validation script for Admin/index, Backend, All and invalid targets.
These checks do not exercise live IAM/GitHub environment APIs, actual AWS apply,
or Windows console behavior.

Final consumer validation used GOWORK=off with published implementation commit
`4d6a774cd137e1255555e70f5d57e8c45213bef7`: all three module race tests and
vet passed; all 22 executable Linux ARM64 Lambda ZIPs were built and inspected.
An unchanged second build reported all 22 targets cached with no compilation.
The Windows deployment CLI cross-compiled and the POSIX launcher help completed.
Workflow routing tests passed. No local module replacements were used.
