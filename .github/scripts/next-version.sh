#!/bin/sh
# Print the next release version derived from Conventional Commits since the
# latest vX.Y.Z tag. Prints nothing when no commit warrants a release.
#
#   feat        -> minor
#   fix / perf  -> patch
#   breaking    -> major (minor while still on v0.x)
set -eu

latest=$(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | head -n1)
if [ -z "$latest" ]; then
    latest=v0.0.0
    range=HEAD
else
    range="$latest..HEAD"
fi

subjects=$(git log --no-merges --format=%s "$range")
bodies=$(git log --no-merges --format=%b "$range")

bump=
if printf '%s\n' "$subjects" | grep -Eq '^[a-z]+(\([^)]*\))?!:' ||
    printf '%s\n' "$bodies" | grep -q '^BREAKING CHANGE:'; then
    bump=major
elif printf '%s\n' "$subjects" | grep -Eq '^feat(\([^)]*\))?:'; then
    bump=minor
elif printf '%s\n' "$subjects" | grep -Eq '^(fix|perf)(\([^)]*\))?:'; then
    bump=patch
fi
[ -n "$bump" ] || exit 0

IFS=. read -r major minor patch <<EOF
${latest#v}
EOF

if [ "$bump" = major ] && [ "$major" -eq 0 ]; then
    bump=minor
fi
case "$bump" in
    major) major=$((major + 1)); minor=0; patch=0 ;;
    minor) minor=$((minor + 1)); patch=0 ;;
    patch) patch=$((patch + 1)) ;;
esac
echo "v$major.$minor.$patch"
