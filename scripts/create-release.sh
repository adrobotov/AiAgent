#!/bin/bash
# create-release.sh - Create a new GitHub release
# Usage: ./create-release.sh [version]
#
# Examples:
#   ./create-release.sh          # Auto-increment patch version
#   ./create-release.sh v1.2.3   # Create specific version

set -euo pipefail

# Get current version from git tags
current_tag=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
current_version=${current_tag#v}

# Parse version components
IFS='.' read -r major minor patch <<< "$current_version"

# Determine new version
if [ $# -eq 0 ]; then
  # Auto-increment patch
  patch=$((patch + 1))
  new_version="v${major}.${minor}.${patch}"
else
  new_version="$1"
  # Validate version format
  if ! echo "$new_version" | grep -qE '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
    echo "Error: Invalid version format. Use vX.Y.Z"
    exit 1
  fi
fi

echo "Creating release: ${new_version}"

# Build the project first
echo "Building project..."
make build VERSION="${new_version}"

# Create git tag
git tag -a "${new_version}" -m "Release ${new_version}"

# Push tag to remote
git push origin "${new_version}"

echo "Tag pushed. GoReleaser will create the GitHub release automatically."
echo "Check https://github.com/${GITHUB_OWNER:-your-username}/$(basename $(git rev-parse --show-toplevel))/releases"
