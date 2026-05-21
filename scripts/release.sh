#!/usr/bin/env bash
set -euo pipefail

# ==========================================================================
# Cut a multica CLI release: cross-compile binaries, package archives, and
# upload assets to GitHub Releases.
#
# This is the manual stand-in for `goreleaser release` — kept because the
# asionius fork has historically published releases this way (no CI, no
# goreleaser binary required), and the .goreleaser.yml workflow gate still
# points at multica-ai. Mirrors the archive layout that .goreleaser.yml
# produces so the existing CLI self-update path keeps working.
#
# Usage:
#   bash scripts/release.sh [--version vX.Y.Z] [--token-file FILE] [--no-upload]
#
# Defaults:
#   --version       latest annotated tag at HEAD  (git describe --exact-match)
#   --token-file    /tmp/gh.txt                   (override with GITHUB_TOKEN env)
#   --owner         asionius                      (from .goreleaser.yml)
#   --repo          multica
#   --dist-dir      /tmp/multica-release-<version>
#   --body-file     release notes file. If omitted, a body is generated from
#                   `git log --pretty='- %s' <prev-tag>..<version>`.
#
# Flags:
#   --no-upload     build + package only; skip GitHub release creation/upload.
#                   Artifacts stay in --dist-dir for inspection.
#   --keep-dist     don't delete --dist-dir on success.
#
# Prerequisites: go (1.26+), tar, zip, sha256sum, curl, jq, python3.
# Permission: PAT needs `repo` scope. `workflow` scope is NOT required since
# this script doesn't touch .github/workflows/*.
# ==========================================================================

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

# ---------- Argument parsing ----------
VERSION=""
TOKEN_FILE="/tmp/gh.txt"
OWNER="asionius"
REPO="multica"
DIST_DIR=""
BODY_FILE=""
NO_UPLOAD=0
KEEP_DIST=0

while [ $# -gt 0 ]; do
  case "$1" in
    --version)     VERSION="$2"; shift 2 ;;
    --token-file)  TOKEN_FILE="$2"; shift 2 ;;
    --owner)       OWNER="$2"; shift 2 ;;
    --repo)        REPO="$2"; shift 2 ;;
    --dist-dir)    DIST_DIR="$2"; shift 2 ;;
    --body-file)   BODY_FILE="$2"; shift 2 ;;
    --no-upload)   NO_UPLOAD=1; shift ;;
    --keep-dist)   KEEP_DIST=1; shift ;;
    -h|--help)     sed -n '1,40p' "$0"; exit 0 ;;
    *)             echo "✗ Unknown flag: $1"; exit 2 ;;
  esac
done

# ---------- Check prerequisites ----------
missing=()
for cmd in go tar zip sha256sum curl jq python3; do
  command -v "$cmd" >/dev/null 2>&1 || missing+=("$cmd")
done
if [ ${#missing[@]} -gt 0 ]; then
  echo "✗ Missing prerequisites: ${missing[*]}"
  exit 1
fi

# ---------- Resolve version ----------
if [ -z "$VERSION" ]; then
  VERSION="$(git describe --tags --exact-match HEAD 2>/dev/null || true)"
  if [ -z "$VERSION" ]; then
    echo "✗ HEAD has no exact-match tag. Pass --version vX.Y.Z, or:"
    echo "    git tag -a v0.3.X -m 'Release v0.3.X'"
    exit 1
  fi
fi
case "$VERSION" in
  v[0-9]*) ;;
  *) echo "✗ Version must look like vX.Y.Z (got: $VERSION)"; exit 1 ;;
esac
VERSION_NO_V="${VERSION#v}"

# ---------- Resolve commit / metadata ----------
COMMIT="$(git rev-parse --short=8 "$VERSION^{commit}")"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
PREV_TAG="$(git describe --tags --abbrev=0 "${VERSION}^" 2>/dev/null || true)"

# ---------- Resolve token (skipped when --no-upload) ----------
TOKEN=""
if [ "$NO_UPLOAD" -eq 0 ]; then
  if [ -n "${GITHUB_TOKEN:-}" ]; then
    TOKEN="$GITHUB_TOKEN"
  elif [ -r "$TOKEN_FILE" ]; then
    TOKEN="$(tr -d '\n\r ' < "$TOKEN_FILE")"
  else
    echo "✗ No token. Set GITHUB_TOKEN env or place a PAT at $TOKEN_FILE"
    echo "  PAT scope required: repo. (workflow scope NOT needed.)"
    exit 1
  fi
fi

# ---------- Sanity: tag is on remote? ----------
# We don't push the tag here — the user does it manually with their PAT.
# But if it's not on remote, the release would 404 on creation, so warn.
if [ "$NO_UPLOAD" -eq 0 ]; then
  REMOTE_TAG_SHA="$(GIT_TERMINAL_PROMPT=0 git ls-remote "https://github.com/${OWNER}/${REPO}.git" "refs/tags/${VERSION}" 2>/dev/null | awk '{print $1}')"
  if [ -z "$REMOTE_TAG_SHA" ]; then
    echo "✗ Tag $VERSION is not on https://github.com/${OWNER}/${REPO}. Push it first:"
    echo "    git push https://<user>:<PAT>@github.com/${OWNER}/${REPO}.git $VERSION"
    exit 1
  fi
fi

# ---------- Resolve dist dir ----------
if [ -z "$DIST_DIR" ]; then
  DIST_DIR="/tmp/multica-release-${VERSION}"
fi
if [ -d "$DIST_DIR" ]; then
  echo "==> Wiping existing $DIST_DIR"
  rm -rf "$DIST_DIR"
fi
mkdir -p "$DIST_DIR/dist"

cleanup() {
  if [ "$KEEP_DIST" -eq 0 ] && [ -d "$DIST_DIR" ]; then
    rm -rf "$DIST_DIR"
  fi
}
trap cleanup EXIT

echo "==> Releasing $VERSION (commit $COMMIT, prev $PREV_TAG)"
echo "    repo:    $OWNER/$REPO"
echo "    distdir: $DIST_DIR"
echo "    upload:  $([ "$NO_UPLOAD" -eq 0 ] && echo yes || echo no)"

# ---------- Cross-compile ----------
# Matches builds: in .goreleaser.yml — main=./cmd/multica, ldflags inject
# main.version/commit/date so `multica version` self-reports correctly.
LDFLAGS="-s -w -X main.version=${VERSION_NO_V} -X main.commit=${COMMIT} -X main.date=${DATE}"
PLATFORMS="darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64"

for platform in $PLATFORMS; do
  GOOS="${platform%/*}"
  GOARCH="${platform#*/}"
  EXT=""
  [ "$GOOS" = "windows" ] && EXT=".exe"
  STAGE="$DIST_DIR/stage-${GOOS}-${GOARCH}"
  mkdir -p "$STAGE"
  echo "==> build $GOOS/$GOARCH"
  (
    cd "$REPO_ROOT/server"
    CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
      go build -ldflags "$LDFLAGS" -o "$STAGE/multica${EXT}" ./cmd/multica
  )
done

# ---------- Package archives ----------
# Two naming schemes per platform — match archives: in .goreleaser.yml.
# Legacy `multica_{os}_{arch}` exists so CLIs <= 0.2.x can still self-update.
# Versioned `multica-cli-{version}-{os}-{arch}` is what current CLIs look for.
for platform in $PLATFORMS; do
  GOOS="${platform%/*}"
  GOARCH="${platform#*/}"
  STAGE="$DIST_DIR/stage-${GOOS}-${GOARCH}"
  if [ "$GOOS" = "windows" ]; then
    LEGACY="multica_${GOOS}_${GOARCH}.zip"
    VERSIONED="multica-cli-${VERSION_NO_V}-${GOOS}-${GOARCH}.zip"
    (cd "$STAGE" && zip -q "$DIST_DIR/dist/$LEGACY" "multica.exe")
    (cd "$STAGE" && zip -q "$DIST_DIR/dist/$VERSIONED" "multica.exe")
  else
    LEGACY="multica_${GOOS}_${GOARCH}.tar.gz"
    VERSIONED="multica-cli-${VERSION_NO_V}-${GOOS}-${GOARCH}.tar.gz"
    (cd "$STAGE" && tar -czf "$DIST_DIR/dist/$LEGACY" "multica")
    (cd "$STAGE" && tar -czf "$DIST_DIR/dist/$VERSIONED" "multica")
  fi
done

# ---------- Smoke-test linux/amd64 binary ----------
SELF_REPORT="$("$DIST_DIR/stage-linux-amd64/multica" version 2>&1 | head -1)"
echo "==> smoke: $SELF_REPORT"
case "$SELF_REPORT" in
  *"$VERSION_NO_V"*) ;;
  *) echo "✗ binary did not self-report $VERSION_NO_V"; exit 1 ;;
esac

# ---------- Checksums ----------
(cd "$DIST_DIR/dist" && sha256sum multica*.tar.gz multica*.zip > checksums.txt)
ARTIFACT_COUNT=$(ls "$DIST_DIR/dist" | wc -l)
echo "==> packed $ARTIFACT_COUNT artifacts in $DIST_DIR/dist"

if [ "$NO_UPLOAD" -eq 1 ]; then
  echo "✓ build complete. --no-upload set, leaving artifacts in $DIST_DIR/dist."
  KEEP_DIST=1  # don't delete on cleanup
  exit 0
fi

# ---------- Resolve release body ----------
BODY=""
if [ -n "$BODY_FILE" ]; then
  BODY="$(cat "$BODY_FILE")"
elif [ -n "$PREV_TAG" ]; then
  BODY="## Changes since ${PREV_TAG}"$'\n\n'
  BODY+="$(git log --pretty='- %s' "${PREV_TAG}..${VERSION}")"
else
  BODY="Release ${VERSION}"
fi

# ---------- Create or fetch release ----------
API="https://api.github.com/repos/${OWNER}/${REPO}"
EXISTING="$(curl -s -H "Authorization: Bearer ${TOKEN}" -H "Accept: application/vnd.github+json" "${API}/releases/tags/${VERSION}")"
RELEASE_ID="$(printf '%s' "$EXISTING" | jq -r '.id // empty')"

if [ -n "$RELEASE_ID" ] && [ "$RELEASE_ID" != "null" ]; then
  echo "==> reusing existing release $VERSION (id $RELEASE_ID)"
else
  echo "==> creating release $VERSION"
  CREATE_PAYLOAD="$(jq -n \
    --arg tag "$VERSION" --arg name "$VERSION" --arg body "$BODY" \
    '{tag_name:$tag, name:$name, body:$body, draft:false, prerelease:false, target_commitish:"main"}')"
  CREATE_RESP="$(curl -s -X POST \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    "${API}/releases" \
    -d "$CREATE_PAYLOAD")"
  RELEASE_ID="$(printf '%s' "$CREATE_RESP" | jq -r '.id // empty')"
  if [ -z "$RELEASE_ID" ] || [ "$RELEASE_ID" = "null" ]; then
    echo "✗ create release failed:"
    printf '%s\n' "$CREATE_RESP" | jq . 2>/dev/null || printf '%s\n' "$CREATE_RESP"
    exit 1
  fi
fi

# ---------- Upload assets ----------
# Existing assets with the same name need to be deleted first; otherwise
# upload returns 422. We re-fetch the asset list because the release may
# have been partially populated by a previous run.
EXISTING_ASSETS_JSON="$(curl -s -H "Authorization: Bearer ${TOKEN}" -H "Accept: application/vnd.github+json" "${API}/releases/${RELEASE_ID}/assets?per_page=100")"

upload_one() {
  local file="$1"
  local name; name="$(basename "$file")"
  local ext="${name##*.}"
  local ct
  case "$ext" in
    txt) ct="text/plain" ;;
    gz)  ct="application/gzip" ;;
    zip) ct="application/zip" ;;
    *)   ct="application/octet-stream" ;;
  esac

  # Delete same-named asset first (idempotency for re-runs).
  local existing_id
  existing_id="$(printf '%s' "$EXISTING_ASSETS_JSON" | jq -r --arg n "$name" '.[] | select(.name==$n) | .id' | head -1)"
  if [ -n "$existing_id" ] && [ "$existing_id" != "null" ]; then
    curl -s -X DELETE -H "Authorization: Bearer ${TOKEN}" "${API}/releases/assets/${existing_id}" > /dev/null
  fi

  local code
  code="$(curl -s -o /tmp/release-upload.json -w '%{http_code}' -X POST \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    -H "Content-Type: $ct" \
    --data-binary "@${file}" \
    "https://uploads.github.com/repos/${OWNER}/${REPO}/releases/${RELEASE_ID}/assets?name=${name}")"
  if [ "$code" = "201" ]; then
    printf '    %-50s OK\n' "$name"
  else
    printf '    %-50s FAIL (%s)\n' "$name" "$code"
    cat /tmp/release-upload.json
    return 1
  fi
}

echo "==> uploading $ARTIFACT_COUNT assets"
for f in "$DIST_DIR/dist/checksums.txt" "$DIST_DIR/dist/"multica_*.tar.gz "$DIST_DIR/dist/"multica_*.zip "$DIST_DIR/dist/"multica-cli-*.tar.gz "$DIST_DIR/dist/"multica-cli-*.zip; do
  [ -f "$f" ] || continue
  upload_one "$f"
done

echo ""
echo "✓ released ${VERSION}"
echo "  https://github.com/${OWNER}/${REPO}/releases/tag/${VERSION}"
