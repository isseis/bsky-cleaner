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
if ! echo "$new_version" | grep -qE '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
    echo "ERROR: version '$new_version' does not match semver format vX.Y.Z" >&2
    exit 1
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
compose_file="$repo_root/docker-compose.yml"
readme_files=("$repo_root/README.md" "$repo_root/README.ja.md")

# update_file replaces the version on lines matching $2 (a sed address
# pattern, e.g. an anchored prefix) in file $1, using replacement pattern
# $3. Fails if no line in the file matches $2, so a renamed field or moved
# line is caught instead of silently doing nothing.
update_file() {
    local file="$1"
    local match_pattern="$2"
    local sed_expr="$3"

    if ! grep -qE "$match_pattern" "$file"; then
        echo "ERROR: expected pattern '$match_pattern' not found in $file" >&2
        exit 1
    fi

    sed -E "$sed_expr" "$file" > "${file}.tmp"
    mv "${file}.tmp" "$file"
    echo "Updated $file"
}

update_file "$compose_file" \
    '^ *image: ghcr\.io/isseis/bsky-cleaner:v[0-9]+\.[0-9]+\.[0-9]+$' \
    "s|(image: ghcr\\.io/isseis/bsky-cleaner:)v[0-9]+\\.[0-9]+\\.[0-9]+|\\1${new_version}|"

for f in "${readme_files[@]}"; do
    update_file "$f" \
        '^VERSION=v[0-9]+\.[0-9]+\.[0-9]+' \
        "s|^(VERSION=)v[0-9]+\\.[0-9]+\\.[0-9]+|\\1${new_version}|"
done

echo
echo "Bumped embedded version to ${new_version} in docker-compose.yml, README.md, README.ja.md."
echo "Review the diff, then:"
echo "  git add docker-compose.yml README.md README.ja.md"
echo "  git commit -m \"release(${new_version}): bump embedded version to ${new_version}\""
echo "  git tag ${new_version}"
echo "  git push && git push --tags"
