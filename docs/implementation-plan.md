# Attestra application setup

Consume Apexfission merged commit 8f7bdade14db through ordinary Go module dependencies. The target originally contained only README.md. No framework source is copied or modified, and no existing Attestra environment is adopted.

1. Add the Attestra module: public health, authenticated self-profile, protected admin section, and declared Lambda build targets. Verify authentication and permission boundaries using the real framework router.
2. Wire Lambda entrypoints to the existing email, Cognito, passkey and challenge implementations. Validate runtime settings before constructing clients; keep admin credentials/configuration separate.
3. Add application-owned Pulumi composition with explicit archives, protected auth/admin resources, scoped admin IAM, and optional custom-domain mapping. Verify registration with Pulumi mocks, not AWS changes.
4. Provide a Go application CLI for init/build/preview/deploy/status/guarded teardown. Discover builds from linked modules. Never substitute example account/domain values for configured deployment targets.
5. Add CI, a manually invoked OIDC deployment workflow, setup instructions, dependency provenance, and a PR. Run tests/race/vet, actual Lambda packaging, and infrastructure mock tests.

The application owns environment config, admin invitations/grants, DNS records and retention decisions. The framework owns authentication behavior and shared contracts. Existing Attestra resource migration remains outside this new-project setup.
