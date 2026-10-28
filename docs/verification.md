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
