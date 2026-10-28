# Verification scope

The repository uses real downloaded framework modules pinned to merged commit `8f7bdade14db41377c3f695f9bc69519f4248ecc`; it needs no sibling framework checkout.

Local verification covers:

- Root unit/integration tests with race detection and `go vet`.
- Actual library routing: public health, unauthenticated profile denial, verified subject response and customer/admin permission separation.
- Runtime settings validation and init no-overwrite behavior.
- Placeholder manifest rejection before cloud operations and linked-module build discovery.
- Infrastructure tests with Pulumi mocks, checking protected resources, expected resource counts, JWT protection for `/v1/me`, admin secret separation and custom-domain mapping.
- Regression tests for the pinned library's managed-login CSP and cookie-authenticated extension routing, including cross-origin mutation rejection.
- SES bootstrap tests verify stable component/child identities and no premature pool creation; full auth registration depends on SES verification.
- Real Linux arm64 Lambda ZIP packaging and executable bootstrap inspection.

No real AWS deployment, Pulumi preview against live state, DNS change, email delivery, administrator invitation, OAuth/browser session or hardware passkey flow was performed. Passing mocks/builds do not certify those integrations. The original authentication repository and library source are unchanged.
