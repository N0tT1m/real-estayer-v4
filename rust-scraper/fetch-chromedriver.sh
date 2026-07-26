#!/usr/bin/env bash
# Download the ChromeDriver build matching the locally installed Chrome.
#
# ChromeDriver must match Chrome's major version, and Chrome auto-updates, so
# this is a recurring chore rather than one-time setup — that is why it is a
# script and not a committed binary. Re-run it whenever the scraper starts
# failing with "This version of ChromeDriver only supports Chrome version N".
#
# Usage:
#   ./fetch-chromedriver.sh              # match installed Chrome
#   ./fetch-chromedriver.sh 141          # force a major version
set -euo pipefail

cd "$(dirname "$0")"

need() { command -v "$1" >/dev/null || { echo "error: $1 is required" >&2; exit 1; }; }
need curl
need unzip

# --- Resolve the platform key used by Chrome for Testing ---
case "$(uname -s)" in
  Linux)  platform=linux64 ;;
  Darwin) [ "$(uname -m)" = arm64 ] && platform=mac-arm64 || platform=mac-x64 ;;
  MINGW*|MSYS*|CYGWIN*) platform=win64 ;;
  *) echo "error: unsupported platform $(uname -s)" >&2; exit 1 ;;
esac

# --- Determine the wanted Chrome major version ---
if [ $# -ge 1 ]; then
  major="$1"
else
  chrome=""
  for candidate in \
    "${CHROME_BINARY_PATH:-}" \
    google-chrome google-chrome-stable chromium chromium-browser \
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
  do
    [ -n "$candidate" ] || continue
    if command -v "$candidate" >/dev/null 2>&1 || [ -x "$candidate" ]; then
      chrome="$candidate"
      break
    fi
  done

  if [ -z "$chrome" ]; then
    echo "error: could not find Chrome. Install it, set CHROME_BINARY_PATH," >&2
    echo "       or pass a major version explicitly: $0 141" >&2
    exit 1
  fi

  version="$("$chrome" --version | grep -oE '[0-9]+(\.[0-9]+)+' | head -1)"
  major="${version%%.*}"
  echo "Detected Chrome $version (major $major) at $chrome"
fi

# --- Look up the matching ChromeDriver download ---
# known-good-versions-with-downloads pins driver builds to Chrome releases, so
# the URL is never guessed.
echo "Resolving ChromeDriver for Chrome $major / $platform ..."
url="$(
  curl -fsSL https://googlechromelabs.github.io/chrome-for-testing/known-good-versions-with-downloads.json |
  python3 -c '
import json, sys
major, platform = sys.argv[1], sys.argv[2]
best = None
for v in json.load(sys.stdin)["versions"]:
    if v["version"].split(".")[0] != major:
        continue
    for d in v.get("downloads", {}).get("chromedriver", []):
        if d["platform"] == platform:
            # Versions are listed oldest-first; keep the newest match.
            best = d["url"]
print(best or "", end="")
  ' "$major" "$platform"
)"

if [ -z "$url" ]; then
  echo "error: no ChromeDriver published for Chrome $major on $platform." >&2
  echo "       See https://googlechromelabs.github.io/chrome-for-testing/" >&2
  exit 1
fi

# --- Download and install next to the scraper binary ---
# ensure_chromedriver_running() in src/routes.rs spawns "./chromedriver"
# relative to the process cwd, and honours CHROME_DRIVER_PATH when set.
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $url"
curl -fsSL "$url" -o "$tmp/chromedriver.zip"
unzip -q -j "$tmp/chromedriver.zip" '*chromedriver*' -d "$tmp"

if [ "$platform" = win64 ]; then
  dest=chromedriver.exe
else
  dest=chromedriver
fi

# A running chromedriver holds the file open on Windows and would be stale
# everywhere else; stop it before replacing the binary.
pkill -f 'chromedriver --port=9515' 2>/dev/null || true

mv "$tmp/$(basename "$dest")" "./$dest"
chmod +x "./$dest"

echo "Installed $(pwd)/$dest"
"./$dest" --version
