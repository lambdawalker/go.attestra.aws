# Android signing associations

Attestra imports the public signing exports created by
[`android-repo-setup`](https://github.com/lambdawalker/gradle.android-repo-setup)
and publishes the approved identities at
`https://<appOrigin>/.well-known/assetlinks.json`. The authentication feature
renders the App Links and passkey relations; no signing key or vault is uploaded.

## Create an export in the Android project

Use a CLI build with public association export support
([implementation](https://github.com/lambdawalker/gradle.android-repo-setup/pull/24)).
Run `android-repo-setup` in the Android checkout and select **APK signing key**.
Creating or verifying the local keystore writes:

```text
~/.projects/<android-repo-name>/secrets/setup-<id>.vault.associations.json
```

The file contains an array of `packageName` and `sha256CertFingerprints` values.
The CLI prints its path. The filename ID identifies the signing vault, not the
certificate fingerprint. Configure the installed application IDs, including any
variant suffix, in the Android setup config or the signing wizard.

Only the public export is needed on the machine running Attestra. For example,
copy it into `~/.projects/<android-repo-name>/secrets/` there, or enter its path
manually. Do not copy the keystore or encrypted vault for this import.
For Play-distributed apps, use the Play app-signing certificate if it differs
from the local/upload certificate.

## Select an export in Attestra

| Operation | macOS / Linux | Windows |
| --- | --- | --- |
| Initial setup / enable hosting | `./setup.sh` | `setup.bat` |
| Revisit completed setup | `./setup.sh -force-setup` | `setup.bat -force-setup` |
| Manage an already hosted file | `./deploy.sh -associations` | `deploy.bat -associations` |

Select the intended **backend environment** in the existing environment flow.
During setup, or after choosing **Add from public JSON** in the association
manager, the picker lists regular files matching
`~/.projects/*/secrets/*.associations.json`. Entries show the Android repository
and export filename. The signing environment and backend environment may have
different names; importing one does not approve every file in that directory.

Choose one export, **Enter another JSON file path**, or **Skip; keep current
approvals**. The selected file is validated, and its application IDs and
SHA-256 fingerprints are shown before setup approval or publication. Imports add
identities without removing existing approvals. Repeat **Add from public JSON**
to stage exports from several apps or signing environments.

Setup asks for approval before adding identities. Enabling hosting creates a
CloudFront site on `appOrigin`, so use a hostname intended for this application.
In the association manager, edits stay in memory until **Publish current list**;
that action shows the exact list and asks for confirmation before updating the
existing association object. It saves the Pulumi configuration for the selected
environment. Exiting without publishing leaves the deployed file unchanged.
Use the explicit removal action to revoke a fingerprint; importing another
export never revokes it.

## Custom paths and skipped setup

The Android CLI's `--state-dir` and `--vault-path` options can place exports
outside the default directory. Use manual path entry for these files; `~/` is
expanded to the current user's home directory. The legacy
`~/.attestra/associations/<environment>/developer-associations.json` schema is
still accepted through manual entry, but that directory is no longer searched.

Discovery does not open unselected exports, vaults, or keystores. Symbolic links
are excluded from automatic discovery, and the selected export must be a regular
file containing a nonempty, valid public association array no larger than 1 MiB.
Malformed unselected exports do not prevent choosing a valid file.

If no exports exist, new setup offers manual entry or a blank answer to skip.
Skipping is remembered locally and leaves hosting unchanged; a newly discovered
export is still offered when setup is revisited. Use `-force-setup` to offer
manual entry again after skipping, including when the export is outside the
default directory and hosting has not been enabled. Existing hosted environments
can also use the association manager to select a custom path at any time.
