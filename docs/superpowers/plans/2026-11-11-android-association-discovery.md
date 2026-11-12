# Android Association Discovery Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans to implement this plan.

**Goal:** Read the new Android CLI public exports in Attestra setup and association management.

**Architecture:** Shared filesystem discovery and bounded JSON validation feed one selection UI. Both existing callers retain their additive merge and approval/publication behavior.

**Tech Stack:** Go, existing Huh UI, pinned auth assetlinks package.

**Spec:** `docs/superpowers/specs/2026-11-11-android-association-discovery-design.md`

## Global Constraints

- Discover `~/.projects/<repo-name>/secrets/*.associations.json` only.
- Read only a selected public file, without reading vaults or signing keys.
- Preserve existing approvals and explicit backend environment selection.
- Keep custom locations available through manual path entry.
- No live deployment or package publication for verification.

## Review Focus

- Multiple Android repositories and signing environments must stay separate.
- Missing roots and skipped first-time setup must not fail or enable hosting.
- Invalid, oversized, nonregular or unrelated files must not enter approvals.
- Unselected malformed exports must not prevent importing a valid selected one.
- Paths containing spaces or glob characters must work on supported systems.

## Task 1: Discover and import public signing exports

**Files:** Add `tools/deploy/internal/workflow/association_files.go` and tests; update `assetlinks.go`, `association_manager.go`, checkpoint skip handling/tests, `docs/android-associations.md`, and `tools/deploy/README.md`.

**Interfaces:** `discoverAssociationFiles(home) ([]string, error)`, `readAssociationFile(path) ([]assetlinks.Application, error)`, and shared selection/preview helpers.

- [x] Add filesystem and schema regression tests, then confirm their initial failure.
- [x] Implement discovery, bounded validation and shared picker; integrate both callers and preserve approvals.
- [x] Document the new export path and usage; run Go tests/vet in all four modules, Node admin tests and helper/archive checks appropriate to the changed code.
- [x] Perform an independent review, fix findings with regression tests, and open a pull request.

Review caught persisted skipping blocking later custom-path imports. Forced setup
now offers manual entry again; regression tests cover approval and rejection.
Follow-up verification traced the infrastructure test's 165/166 inventory mismatch
to a missing ownership hook on the card-capture comparison policy. The fix lives
in [card-capture PR #44](https://github.com/lambdawalker/go.appexfission.aws.card-capture/pull/44),
and Attestra pins that corrected infrastructure module. The original completeness
assertion is retained, with diagnostics naming any omitted resources.
Validation results are recorded in the pull request.
