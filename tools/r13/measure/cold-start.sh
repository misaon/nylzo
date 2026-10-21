#!/usr/bin/env bash
# Test (b): studený start. Po --idle-minutes nečinnosti změří první odpověď GET /start.
set -euo pipefail
. "$(dirname "$0")/lib.sh"

usage() {
	cat <<'USAGE'
Použití: cold-start.sh <base-url> [--idle-minutes 16] [--repeat 1] [--limit-s 3] [--out SOUBOR.csv]

1. Pošle zahřívací GET /start a zapamatuje si started_at instance.
2. Čeká --idle-minutes (výchozí 16, kontejner usíná po 15 min) a po tu dobu se URL nesmí
   nic dotknout (zavři záložky prohlížeče, žádný uptime monitor).
3. Změří první GET /start: čas do první odpovědi (curl time_starttransfer), celkový čas
   a uptime_ms procesu v odpovědi (kolik ms po startu procesu byl požadavek obsloužen).
4. Hned poté změří druhý, teplý požadavek pro srovnání.

Verdikt PASS: nová instance (jiné started_at) a time_total < --limit-s (výchozí 3 s).
Pokud started_at zůstalo stejné, instance neusnula a měření se nepočítá.
USAGE
}

[ $# -ge 1 ] || { usage >&2; exit 2; }
case "$1" in -h | --help) usage; exit 0 ;; esac

BASE="$(r13_base_url "$1")"
shift
IDLE_MINUTES=16
REPEAT=1
LIMIT_S=3
OUT=""
while [ $# -gt 0 ]; do
	case "$1" in
	--idle-minutes) IDLE_MINUTES="$2"; shift 2 ;;
	--repeat) REPEAT="$2"; shift 2 ;;
	--limit-s) LIMIT_S="$2"; shift 2 ;;
	--out) OUT="$2"; shift 2 ;;
	-h | --help) usage; exit 0 ;;
	*) usage >&2; exit 2 ;;
	esac
done
[ -n "$OUT" ] || OUT="$(r13_results_dir)/cold-start-$(date -u +%Y%m%dT%H%M%SZ).csv"

r13_need curl sed awk mktemp
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fetch_start() {
	local label="$1"
	local resp="$TMP/$label.json" stats
	stats="$(r13_curl_json "$resp" --max-time 60 "$BASE/start")"
	local body
	body="$(cat "$resp")"
	printf '%s %s %s %s\n' "$stats" "$(r13_json_str "$body" started_at)" "$(r13_json_num "$body" uptime_ms)" "$(r13_json_str "$body" instance)"
}

printf 'ts,round,phase,http_code,time_namelookup,time_connect,time_appconnect,time_starttransfer,time_total,started_at,uptime_ms,instance,verdict\n' >"$OUT"

round=1
while [ "$round" -le "$REPEAT" ]; do
	r13_log "kolo $round: zahřívací požadavek"
	warm="$(fetch_start warm)"
	warm_started="$(printf '%s' "$warm" | awk '{print $9}')"
	r13_log "instance běží, started_at=$warm_started (http=${warm%% *})"
	printf '%s,%s,warm,%s\n' "$(r13_ts)" "$round" "$(printf '%s' "$warm" | awk '{printf "%s,%s,%s,%s,%s,%s,%s,%s,%s,", $1,$2,$3,$4,$5,$6,$9,$10,$11}')" >>"$OUT"

	r13_sleep_countdown $((IDLE_MINUTES * 60)) "nečinnost"

	r13_log "první požadavek po nečinnosti"
	cold="$(fetch_start cold)"
	# shellcheck disable=SC2086
	set -- $cold
	code="${1:-}" t_dns="${2:-}" t_conn="${3:-}" t_tls="${4:-}" t_first="${5:-}" t_total="${6:-}" cold_started="${9:-}" uptime_ms="${10:-}" instance="${11:-}"
	verdict="FAIL"
	if [ "$code" = "200" ] && [ "$cold_started" != "$warm_started" ] && r13_float_lt "$t_total" "$LIMIT_S"; then
		verdict="PASS"
	elif [ "$code" = "200" ] && [ "$cold_started" = "$warm_started" ]; then
		verdict="NEPLATNE-instance-neusnula"
	fi
	printf 'studený start: http=%s dns=%ss connect=%ss tls=%ss first_byte=%ss total=%ss started_at=%s uptime_ms=%s instance=%s -> %s\n' \
		"$code" "$t_dns" "$t_conn" "$t_tls" "$t_first" "$t_total" "$cold_started" "$uptime_ms" "$instance" "$verdict"
	printf '%s,%s,cold,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n' "$(r13_ts)" "$round" "$code" "$t_dns" "$t_conn" "$t_tls" "$t_first" "$t_total" "$cold_started" "$uptime_ms" "$instance" "$verdict" >>"$OUT"

	hot="$(fetch_start hot)"
	# shellcheck disable=SC2086
	set -- $hot
	printf 'teplý požadavek: http=%s first_byte=%ss total=%ss uptime_ms=%s\n' "${1:-}" "${5:-}" "${6:-}" "${10:-}"
	printf '%s,%s,hot,%s,%s,%s,%s,%s,%s,%s,%s,%s,\n' "$(r13_ts)" "$round" "${1:-}" "${2:-}" "${3:-}" "${4:-}" "${5:-}" "${6:-}" "${9:-}" "${10:-}" "${11:-}" >>"$OUT"
	round=$((round + 1))
done

r13_log "hotovo, CSV: $OUT"
