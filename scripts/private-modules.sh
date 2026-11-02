#!/usr/bin/env bash
set -euo pipefail
# Optional on machines already configured with gh auth setup-git. Actions callers
# supply a read-only token as FEATURES_READ_TOKEN; no credential is written here.
if [[ -n "${FEATURES_READ_TOKEN:-}" ]]; then
  git config --global credential.https://github.com.helper '!f() { if test "$1" = get; then printf "%s\n" "username=x-access-token" "password=$FEATURES_READ_TOKEN"; fi; }; f'
fi
