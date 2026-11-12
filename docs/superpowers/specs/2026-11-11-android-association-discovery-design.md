# Discover Android CLI association exports

The Android repository setup CLI now writes public import arrays beside its
signing vaults: `~/.projects/<repo-name>/secrets/setup-<id>.vault.associations.json`.
Attestra must consume these from both initial setup and its association manager.

Discover regular `*.associations.json` files directly inside each project's
`secrets` directory. List their repository and complete filename in stable order.
Read only the operator-selected file; do not inspect vaults or keystores, follow
discovery symlinks, or import every discovered project's identities. No signing
environment is inferred from the filename or mapped to a backend environment.

Use a shared picker with manual path entry and skip choices. Custom state/vault
locations remain supported through manual entry. Validate a bounded public JSON
array before merging. Existing approvals are retained; setup still explicitly
approves changes, while the manager stages changes until its existing Publish
confirmation. Cancellation and invalid files must not alter approvals.

Replace the old automatic `~/.attestra/associations/<environment>` lookup. Existing
JSON files can still be imported manually. Document generation, selection and
first-time hosting; do not change the auth feature or Composer, deploy resources,
or change the CLI export contract.

Approach: shared discovery and import helpers fit the existing Go operator.
Hardcoding an Android repo or matching signing names to backend environments
would incorrectly select identities. Importing private signing state is unnecessary.
