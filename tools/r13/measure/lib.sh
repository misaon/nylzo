# shellcheck shell=bash
# Sdílené funkce měřicích skriptů R13. Načítá se přes `. "$(dirname "$0")/lib.sh"`.

r13_need() {
	local cmd
	for cmd in "$@"; do
		if ! command -v "$cmd" >/dev/null 2>&1; then
			printf 'chybí příkaz: %s\n' "$cmd" >&2
			exit 2
		fi
	done
}

r13_ts() {
	date -u +%Y-%m-%dT%H:%M:%SZ
}

r13_log() {
	printf '%s %s\n' "$(r13_ts)" "$*" >&2
}

r13_die() {
	printf 'chyba: %s\n' "$*" >&2
	exit 1
}

r13_base_url() {
	local url="$1"
	case "$url" in
	http://* | https://*) ;;
	*) url="https://$url" ;;
	esac
	printf '%s' "${url%/}"
}

r13_host_from_url() {
	local url="$1"
	url="${url#http://}"
	url="${url#https://}"
	url="${url%%/*}"
	printf '%s' "${url%%:*}"
}

r13_json_num() {
	printf '%s' "$1" | sed -n "s/.*\"$2\":\([0-9.eE+-]*\).*/\1/p" | head -n 1
}

r13_json_str() {
	printf '%s' "$1" | sed -n "s/.*\"$2\":\"\([^\"]*\)\".*/\1/p" | head -n 1
}

r13_results_dir() {
	local dir
	dir="$(cd "$(dirname "$0")/.." && pwd)/results"
	mkdir -p "$dir"
	printf '%s' "$dir"
}

r13_sleep_countdown() {
	local seconds="$1" label="$2" left
	left="$seconds"
	while [ "$left" -gt 0 ]; do
		if [ "$left" -ge 60 ]; then
			r13_log "$label: zbývá $((left / 60)) min"
			sleep 60
			left=$((left - 60))
		else
			sleep "$left"
			left=0
		fi
	done
}

r13_float_lt() {
	awk -v a="$1" -v b="$2" 'BEGIN { exit !(a + 0 < b + 0) }'
}

r13_curl_json() {
	local out="$1"
	shift
	: >"$out"
	curl -sS -o "$out" -w '%{http_code} %{time_namelookup} %{time_connect} %{time_appconnect} %{time_starttransfer} %{time_total} %{size_upload} %{exitcode}' "$@" || true
}
