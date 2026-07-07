#!/usr/bin/env bash
set -euo pipefail

SERVER=""
TOKEN=""
INSECURE=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --server) SERVER="$2"; shift 2;;
    --token) TOKEN="$2"; shift 2;;
    --insecure) INSECURE="1"; shift;;
    *) echo "unknown arg: $1"; exit 1;;
  esac
done

if [[ -z "$SERVER" || -z "$TOKEN" ]]; then
  echo "usage: install.sh --server <url> --token <token> [--insecure]"
  exit 1
fi

CURL_OPTS=()
ENROLL_OPTS=()
if [[ -n "$INSECURE" ]]; then
  CURL_OPTS+=(-k)
  ENROLL_OPTS+=(--insecure-skip-tls-verify)
  echo "warning: --insecure set, TLS certificate verification is disabled for this install"
fi

if [[ "$(id -u)" -ne 0 ]]; then
  echo "this script must be run as root (try: sudo bash -s -- ...)"
  exit 1
fi

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) GOARCH=amd64 ;;
  aarch64|arm64) GOARCH=arm64 ;;
  *) echo "unsupported architecture: $ARCH"; exit 1 ;;
esac

BIN_URL="$SERVER/api/v1/downloads/sentinel-agent-linux-$GOARCH"
echo "Downloading agent from $BIN_URL"
curl "${CURL_OPTS[@]}" -fsSL "$BIN_URL" -o /usr/local/bin/sentinel-agent
chmod +x /usr/local/bin/sentinel-agent

/usr/local/bin/sentinel-agent enroll --server "$SERVER" --token "$TOKEN" "${ENROLL_OPTS[@]}"
/usr/local/bin/sentinel-agent install
/usr/local/bin/sentinel-agent start

echo "Sentinel agent installed and started."
