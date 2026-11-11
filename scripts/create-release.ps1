# create-release.ps1 - Create a new GitHub release (PowerShell)
# Usage: .\scripts\create-release.ps1 [version]
#
# Examples:
#   .\scripts\create-release.ps1          # Auto-increment patch version
#   .\scripts\create-release.ps1 v1.2.3   # Create specific version

param(
    [string]$Version
)

$ErrorActionPreference = "Stop"

# Get current version from git tags
$currentTag = git describe --tags --abbrev=0 2>$null
if (-not $currentTag) {
    $currentTag = "v0.0.0"
}
$currentVersion = $currentTag -replace '^v', ''

# Parse version components
$parts = $currentVersion -split '\.'
$major = [int]$parts[0]
$minor = [int]$parts[1]
$patch = [int]$parts[2]

# Determine new version
if (-not $Version) {
    # Auto-increment patch
    $patch++
    $newVersion = "v${major}.${minor}.${patch}"
} else {
    $newVersion = $Version
    # Validate version format
    if ($newVersion -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+$') {
        Write-Error "Invalid version format. Use vX.Y.Z"
        exit 1
    }
}

Write-Host "Creating release: $newVersion" -ForegroundColor Green

# Build the project first
Write-Host "Building project..." -ForegroundColor Cyan
& make build VERSION=$newVersion
if ($LASTEXITCODE -ne 0) {
    Write-Error "Build failed"
    exit 1
}

# Create git tag
git tag -a "$newVersion" -m "Release $newVersion"

# Push tag to remote
git push origin "$newVersion"

Write-Host "Tag pushed. GoReleaser will create the GitHub release automatically." -ForegroundColor Green
Write-Host "Check https://github.com/$(git remote get-url origin | Select-String -Pattern 'github.com[:/](.+)/' -AllMatches | ForEach-Object { $_.Matches.Groups[1].Value })/releases" -ForegroundColor Yellow
