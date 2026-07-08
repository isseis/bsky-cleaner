#!/usr/bin/env bash
# check-existing-tag.sh — fail-closed check for an existing GHCR tag.
#
# Usage: docker manifest inspect ghcr.io/isseis/bsky-cleaner:<tag> 2>&1 \
#          | check-existing-tag.sh "$EXIT_CODE" \
#   EXIT_CODE=$?
# Note: After the pipeline, $? is the exit code of the last command in the
# pipeline (check-existing-tag.sh itself), not of docker manifest inspect.
# Use the || pattern instead: capture EXIT_CODE separately.
#
# The script reads the exit code of docker manifest inspect as $1 and its
# combined stderr+stdout on stdin. It returns:
#   0 — tag does NOT exist (safe to proceed)
#   1 — tag exists, or the check was inconclusive (fail-closed)
#
# The "not found" string is GHCR's equivalent of "manifest unknown".
# It was determined experimentally by running
#   docker manifest inspect ghcr.io/isseis/bsky-cleaner:nonexistent-tag
# and capturing the error output. That experiment used an existing package
# with a missing tag; it has NOT been verified against the "package does not
# exist at all yet" case (e.g. the very first release), which some registries
# report with a different error class. Until confirmed, treat that scenario
# as covered by the fail-closed default (this script exits 1) rather than by
# this specific string match.

set -o errexit
set -o nounset
set -o pipefail

exit_code="${1:-}"
if [ -z "$exit_code" ]; then
    # No exit code argument — inconclusive, fail-closed.
    exit 1
fi
input="$(cat)"

# Tag exists — fail-closed.
if [ "$exit_code" = "0" ]; then
    exit 1
fi

# Check whether the error message confirms the tag does not exist.
# GHCR returns "manifest unknown" when the tag is not found.
if echo "$input" | grep -q "manifest unknown"; then
    exit 0
fi

# Any other failure (rate limit, registry 5xx, network error, etc.) is
# inconclusive — fail-closed.
exit 1
