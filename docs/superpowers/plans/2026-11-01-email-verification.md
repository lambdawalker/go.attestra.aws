# Isolated email verification implementation plan

Goal: Attestra composes authentication and email verification as two independently maintained features.

Architecture: Move the existing proof state machine, HTTP handlers and AWS adapters to verifyemail. Its generic email.Identity interface remains injectable in Go; the native adapter consumes an explicit Cognito enrollment contract from authentication. It owns a separate encrypted/PITR proof table, pinned Secrets Manager key, three Lambdas, scoped permissions and the existing POST /signup, /resend and /confirm routes on the supplied API. Authentication retains Cognito, SES identity (also used for sign-in OTP), grant/enrollment storage, capture, passkeys and runtime policy. The application explicitly injects the integration and finalizes a combined reset inventory after both resource groups exist.

Alternatives considered: keeping verification inside authentication would not demonstrate two features; a remote identity RPC would add a new service boundary and failure mode. Use the existing Go interface with a configurable Cognito adapter and injected native resource references. No auth runtime dependency from verification. No panel is deployed; the app owns configuration.

- [x] Preserve all existing A+B/B+C, replay, resend, expiry, limits and secret tests in the extracted repository. Add independent proof/grant store regression.
- [x] Add declared integration/configuration contracts and verification-native preflight/mocks.
- [x] Remove extracted commands/resources from auth; export integration outputs and reset contribution. Verify 17 remaining functions.
- [x] Compose both features in app, include both manifests/build tools, validate both configurations before registration, combine reset coverage. Test missing binding, native wiring, IAM, unchanged routes and 20 combined functions.
- [x] Publish pinned branches/PRs without deploying AWS. Validate dependency-only consumption and archive builds. Document private module access and migration (pending proofs stay with their existing deployment).

Review focus: no cyclic Go dependency; no implicit grant-table fallback; no raw key delivery; all verification writers paused/reset; no AWS resource registrations before complete app preflight; dependency pins and private-module access remain explicit. Existing domains, users, data and proof transactions are not migrated by this change.


Validation: all eight Go modules passed race tests and vet in the development workspace. Application root, infrastructure, control-plane infrastructure and deployment tool also pass with GOWORK=off against immutable module packages, without cross-repository replacements. The private source was made available through the connected repository and a temporary local module proxy for this environment; normal developer/CI fetching requires the documented private-repository read access. Admin Node tests (5), Python helper tests (9) and 21 Linux/ARM64 Lambda archive checks pass. Independent review found the reset worker's fixed two-table guard; ParseManifest now supports the composed inventory and the integration test runs the exact generated manifest through that startup parser. The read-only app-config function remains outside the 18-function pause inventory. Workflow YAML parsing passed. No AWS deployment/migration/reset was run.
