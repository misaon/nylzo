#!/usr/bin/env bash
# Test (d): vyjednání post-kvantové hybridní skupiny X25519MLKEM768 na TLS edge instance.
set -euo pipefail
. "$(dirname "$0")/lib.sh"

usage() {
	cat <<'USAGE'
Použití: tls-pq.sh <host-nebo-url> [--port 443] [--openssl /cesta/k/openssl]

Spustí `openssl s_client -groups X25519MLKEM768` proti doméně instance a vypíše
vyjednanou skupinu (řádek „Negotiated TLS1.3 group“). Pro srovnání spustí i výchozí
vyjednávání bez -groups a kontrolní běh s -groups X25519.

Potřebuje OpenSSL 3.5 nebo novější (LibreSSL v macOS nestačí):
  brew install openssl@3    (skript ho najde v /opt/homebrew/opt/openssl@3/bin sám)
USAGE
}

[ $# -ge 1 ] || { usage >&2; exit 2; }
case "$1" in -h | --help) usage; exit 0 ;; esac

HOST="$(r13_host_from_url "$1")"
shift
PORT=443
OPENSSL=""
while [ $# -gt 0 ]; do
	case "$1" in
	--port) PORT="$2"; shift 2 ;;
	--openssl) OPENSSL="$2"; shift 2 ;;
	-h | --help) usage; exit 0 ;;
	*) usage >&2; exit 2 ;;
	esac
done

if [ -z "$OPENSSL" ]; then
	for candidate in /opt/homebrew/opt/openssl@3/bin/openssl /usr/local/opt/openssl@3/bin/openssl openssl; do
		if command -v "$candidate" >/dev/null 2>&1; then
			OPENSSL="$candidate"
			break
		fi
	done
fi
[ -n "$OPENSSL" ] || r13_die "openssl nenalezen"

version_line="$("$OPENSSL" version)"
case "$version_line" in
OpenSSL\ 3.[5-9]* | OpenSSL\ 3.[1-9][0-9]* | OpenSSL\ [4-9]*) ;;
*) r13_die "potřebuji OpenSSL >= 3.5, mám: $version_line (zkus --openssl /opt/homebrew/opt/openssl@3/bin/openssl)" ;;
esac
r13_log "používám $OPENSSL ($version_line), cíl $HOST:$PORT"

run_case() {
	local label="$1"
	shift
	local out
	out="$("$OPENSSL" s_client -connect "$HOST:$PORT" -servername "$HOST" -brief "$@" </dev/null 2>&1 || true)"
	local proto group cipher
	proto="$(printf '%s\n' "$out" | sed -n 's/^Protocol version: //p' | head -n 1)"
	cipher="$(printf '%s\n' "$out" | sed -n 's/^Ciphersuite: //p' | head -n 1)"
	group="$(printf '%s\n' "$out" | sed -n 's/^Negotiated TLS1.3 group: //p' | head -n 1)"
	if [ -z "$proto" ]; then
		printf '%-24s CHYBA: %s\n' "$label" "$(printf '%s\n' "$out" | grep -v '^$' | tail -n 3 | tr '\n' ' ')"
		return
	fi
	printf '%-24s protocol=%s cipher=%s group=%s\n' "$label" "$proto" "$cipher" "${group:-?}"
	case "$label" in
	forced-X25519MLKEM768)
		if [ "$group" = "X25519MLKEM768" ]; then
			printf '%-24s PASS: edge vyjednal hybridní ML-KEM\n' ""
		else
			printf '%-24s FAIL: edge hybridní ML-KEM nenabízí (AK26 v návrhu zůstává otevřené)\n' ""
		fi
		;;
	esac
}

run_case "forced-X25519MLKEM768" -groups X25519MLKEM768
run_case "default-groups"
run_case "control-X25519" -groups X25519
