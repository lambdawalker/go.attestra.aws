# Authentication Feature Integration Plan

> Implement against the feature's docs/composition-design.md and the existing auth source baseline; use test-first changes and independent final review.

**Goal:** Make authentication independently reusable and let this repository own application composition and deployment.

**Architecture:** A pinned runtime feature plus optional native infrastructure module, application-owned control plane/operator, and composer preflight/bounded Pulumi factories. Existing business behavior and data contracts stay intact.

**Constraints:** Go 1.27.3; source baseline 4607807; no deployed state/config files copied; no cloud mutation; feature never imports application; no sibling-clone dependency in published app.

- [ ] Extract feature runtime modules/commands and preserve existing tests.
- [ ] Publish artifact/configuration/identity declarations and validate real app injection.
- [ ] Extract reusable native infrastructure and replace plaintext proof-key delivery with secret-scoped cold-start retrieval.
- [ ] Move admin/index/operator ownership to application and build feature command packages from pinned dependencies.
- [ ] Wire application/backend Pulumi through composer and add standalone feature path.
- [ ] Verify runtime, composition, mocks, operator/build/archive/UI helpers and migration documentation.
- [ ] Review, pin published module commits and push coordinated feature/application PRs, retaining legacy runtime unchanged.
