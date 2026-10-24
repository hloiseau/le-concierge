#!/usr/bin/env bash
set -euo pipefail
edge=false
if [[ "$GITHUB_EVENT_NAME" == push && "$GITHUB_REF" == refs/heads/main ]]; then
  # The image job holds the publication lock while checking and pushing.
  main_ref="$(git ls-remote --exit-code origin refs/heads/main)"
  if [[ "$main_ref" == "$GITHUB_SHA"$'\trefs/heads/main' ]]; then
    edge=true
  fi
fi
printf 'edge=%s\n' "$edge"
