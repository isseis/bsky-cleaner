#!/usr/bin/env bash
# bump-release-version.sh — update the release version embedded in
# docker-compose.yml and README.md/README.ja.md ahead of a release.
#
# Usage:
#   scripts/bump-release-version.sh vX.Y.Z
#
# Updates in place:
#   - docker-compose.yml:       image: ghcr.io/isseis/bsky-cleaner:vX.Y.Z
#   - README.md / README.ja.md: VERSION=vX.Y.Z (Docker Compose setup step)
#
# Does not commit or tag — review the diff, then commit, tag, and push
# yourself (see docs/design/docker_deployment.md).

set -o errexit
set -o nounset
set -o pipefail

if [ "$#" -ne 1 ]; then
    echo "Usage: $0 vX.Y.Z" >&2
    exit 1
fi

new_version="$1"
# Validate with bash's [[ =~ ]] rather than `echo "$1" | grep`: grep matches
# line by line, so an argument like $'v1.2.3\n<payload>' would pass on its
# clean first line and smuggle the payload (including a newline) into the sed
# program below. [[ =~ ]] anchors against the whole string, so a multi-line
# or otherwise malformed argument is rejected outright — and because the
# accepted value is then guaranteed to be exactly vN.N.N, it cannot inject
# sed commands when interpolated into the substitution expressions.
if [[ ! "$new_version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "ERROR: version '$new_version' does not match semver format vX.Y.Z" >&2
    exit 1
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
compose_file="$repo_root/docker-compose.yml"
readme_files=("$repo_root/README.md" "$repo_root/README.ja.md")

# update_file replaces the version on lines matching $2 (a sed extended
# regex, anchored to the exact line shape expected) in file $1, using
# replacement pattern $3. The same anchored $2 is used for both the
# existence check and the substitution, so a match is never wider (e.g. an
# unanchored substring match inside an unrelated comment) than what was
# actually verified to be present. Fails if no line in the file matches
# $2, so a renamed field, moved line, or missing file is caught instead of
# silently doing nothing.
update_file() {
    local file="$1"
    local match_pattern="$2"
    local sed_expr="$3"

    if [ ! -f "$file" ]; then
        echo "ERROR: file not found: $file" >&2
        exit 1
    fi

    if ! grep -qE "$match_pattern" "$file"; then
        echo "ERROR: expected pattern '$match_pattern' not found in $file" >&2
        exit 1
    fi

    # Write to a randomized temp file in the same directory (same filesystem,
    # so the final mv is atomic) rather than a predictable "${file}.tmp".
    # A predictable path lets an attacker pre-plant a symlink there that the
    # `>` redirect would follow, overwriting an arbitrary file outside the
    # repo. mktemp creates a fresh non-symlink file, closing that vector.
    local tmp
    tmp="$(mktemp "${file}.XXXXXX")"
    sed -E "$sed_expr" "$file" > "$tmp"
    mv "$tmp" "$file"
    echo "Updated $file"
}

update_file "$compose_file" \
    '^( *image: ghcr\.io/isseis/bsky-cleaner:)v[0-9]+\.[0-9]+\.[0-9]+$' \
    "s|^( *image: ghcr\\.io/isseis/bsky-cleaner:)v[0-9]+\\.[0-9]+\\.[0-9]+\$|\\1${new_version}|"

for f in "${readme_files[@]}"; do
    update_file "$f" \
        '^(VERSION=)v[0-9]+\.[0-9]+\.[0-9]+( |$)' \
        "s#^(VERSION=)v[0-9]+\\.[0-9]+\\.[0-9]+( |\$)#\\1${new_version}\\2#"
done

echo
echo "Bumped embedded version to ${new_version} in docker-compose.yml, README.md, README.ja.md."
echo "Review the diff, then:"
echo "  git add docker-compose.yml README.md README.ja.md"
echo "  git commit -m \"release(${new_version}): bump embedded version to ${new_version}\""
echo "  git tag ${new_version}"
echo "  git push && git push --tags"
