#!/bin/sh
# airval installer: downloads the latest (or a chosen) release from GitHub, verifies its checksum and installs the
# single `airval` binary.
#
#   curl -fsSL https://raw.githubusercontent.com/prat31/airval/main/scripts/install.sh | sh
#   curl -fsSL .../install.sh | sh -s -- --version v0.1.0 --dir ~/bin
#
# Options (or env): --version / AIRVAL_VERSION   release tag, default latest
#                   --dir / AIRVAL_INSTALL_DIR   install directory, default /usr/local/bin (if writable) else ~/.local/bin
set -eu

REPO="prat31/airval"
VERSION="${AIRVAL_VERSION:-latest}"
DIR="${AIRVAL_INSTALL_DIR:-}"

say() { printf '%s\n' "$*"; }
die() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --version) [ $# -ge 2 ] || die "--version needs a value"; VERSION="$2"; shift 2 ;;
    --dir) [ $# -ge 2 ] || die "--dir needs a value"; DIR="$2"; shift 2 ;;
    -h|--help) sed -n '2,10p' "$0" 2>/dev/null | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown option: $1" ;;
  esac
done

# --- platform ---------------------------------------------------------------------------------
case "$(uname -s)" in
  Linux) OS=linux ;;
  Darwin) OS=darwin ;;
  MINGW*|MSYS*|CYGWIN*) die "on Windows download the zip from https://github.com/$REPO/releases" ;;
  *) die "unsupported OS: $(uname -s)" ;;
esac
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *) die "unsupported architecture: $(uname -m)" ;;
esac

# --- tools ------------------------------------------------------------------------------------
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
  fetch_stdout() { curl -fsSL "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -q "$1" -O "$2"; }
  fetch_stdout() { wget -qO- "$1"; }
else
  die "curl or wget is required"
fi
if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
  die "sha256sum or shasum is required"
fi
command -v tar >/dev/null 2>&1 || die "tar is required"

# --- resolve version --------------------------------------------------------------------------
if [ "$VERSION" = "latest" ]; then
  VERSION=$(fetch_stdout "https://api.github.com/repos/$REPO/releases/latest" |
    sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)
  [ -n "$VERSION" ] || die "could not determine the latest release (no releases yet?)"
fi
case "$VERSION" in v*) ;; *) VERSION="v$VERSION" ;; esac

BASE="${AIRVAL_DOWNLOAD_BASE:-https://github.com/$REPO/releases/download/$VERSION}" # override: mirrors, tests
ARCHIVE="airval_${OS}_${ARCH}.tar.gz"

TMP=$(mktemp -d 2>/dev/null || mktemp -d -t airval)
trap 'rm -rf "$TMP"' EXIT INT TERM

say "Installing airval $VERSION ($OS/$ARCH)"
fetch "$BASE/$ARCHIVE" "$TMP/$ARCHIVE" || die "download failed: $BASE/$ARCHIVE"
fetch "$BASE/checksums.txt" "$TMP/checksums.txt" || die "download failed: $BASE/checksums.txt"

# --- verify -----------------------------------------------------------------------------------
want=$(awk -v f="$ARCHIVE" '$2 == f || $2 == "*" f { print $1; exit }' "$TMP/checksums.txt")
[ -n "$want" ] || die "$ARCHIVE not listed in checksums.txt"
got=$(sha256 "$TMP/$ARCHIVE")
[ "$want" = "$got" ] || die "checksum mismatch for $ARCHIVE (expected $want, got $got)"
say "Checksum OK"

# --- install ----------------------------------------------------------------------------------
tar -xzf "$TMP/$ARCHIVE" -C "$TMP" airval || die "archive does not contain the airval binary"
if [ -z "$DIR" ]; then
  if [ -w /usr/local/bin ] 2>/dev/null; then DIR=/usr/local/bin
  elif [ "$(id -u)" -eq 0 ]; then DIR=/usr/local/bin
  else DIR="$HOME/.local/bin"; fi
fi
mkdir -p "$DIR" 2>/dev/null || true
if [ -w "$DIR" ]; then
  install -m 0755 "$TMP/airval" "$DIR/airval"
elif command -v sudo >/dev/null 2>&1; then
  say "Using sudo to write to $DIR"
  sudo install -m 0755 "$TMP/airval" "$DIR/airval"
else
  die "cannot write to $DIR; re-run with --dir <writable dir>"
fi

say "Installed $DIR/airval"
case ":$PATH:" in
  *":$DIR:"*) ;;
  *) say "Note: $DIR is not on your PATH. Add: export PATH=\"$DIR:\$PATH\"" ;;
esac
say "Next: airval dev    # UI at http://localhost:8080"
