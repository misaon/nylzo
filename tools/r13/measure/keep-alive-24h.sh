#!/usr/bin/env bash
# Test (c): jeden triviální požadavek za minutu po dobu 24 h (útok „drž vše vzhůru“).
set -euo pipefail
. "$(dirname "$0")/lib.sh"

usage() {
	cat <<'USAGE'
Použití: keep-alive-24h.sh <base-url> [--hours 24] [--interval 60] [--out SOUBOR.csv]

Každých --interval sekund (výchozí 60) pošle GET /start a zapíše řádek CSV:
ts, http_code, time_total, instance, uptime_ms, started_at. Změna started_at znamená,
že instance mezitím usnula a startovala znovu (při 1 požadavku za minutu by neměla).

Cena se neměří tady: před startem a po skončení si zapiš spotřebu v konzoli Scaleway
(Billing > Consumption, Serverless Containers), rozdíl je cena 24 h „drž vzhůru“.
Na macOS se skript sám obalí do `caffeinate -i`, aby notebook neusnul
(vypnutí: R13_NO_CAFFEINATE=1). Notebook musí mít stabilní síť po celou dobu.
USAGE
}

[ $# -ge 1 ] || { usage >&2; exit 2; }
case "$1" in -h | --help) usage; exit 0 ;; esac

if [ -z "${R13_NO_CAFFEINATE:-}" ] && [ -z "${R13_CAFFEINATED:-}" ] && command -v caffeinate >/dev/null 2>&1; then
	export R13_CAFFEINATED=1
	exec caffeinate -i "$0" "$@"
fi

BASE="$(r13_base_url "$1")"
shift
HOURS=24
INTERVAL=60
OUT=""
while [ $# -gt 0 ]; do
	case "$1" in
	--hours) HOURS="$2"; shift 2 ;;
	--interval) INTERVAL="$2"; shift 2 ;;
	--out) OUT="$2"; shift 2 ;;
	-h | --help) usage; exit 0 ;;
	*) usage >&2; exit 2 ;;
	esac
done
[ -n "$OUT" ] || OUT="$(r13_results_dir)/keep-alive-$(date -u +%Y%m%dT%H%M%SZ).csv"

r13_need curl sed awk mktemp sort
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

total=$((HOURS * 3600 / INTERVAL))
printf 'ts,n,http_code,time_total_s,instance,uptime_ms,started_at,restart\n' >"$OUT"
r13_log "cíl $BASE, $total požadavků po $INTERVAL s, CSV $OUT"

n=1
errors=0
restarts=0
last_started=""
while [ "$n" -le "$total" ]; do
	stats="$(r13_curl_json "$TMP/resp.json" --max-time 30 "$BASE/start")"
	body="$(cat "$TMP/resp.json")"
	code="${stats%% *}"
	t_total="$(printf '%s' "$stats" | awk '{print $6}')"
	started="$(r13_json_str "$body" started_at)"
	uptime_ms="$(r13_json_num "$body" uptime_ms)"
	instance="$(r13_json_str "$body" instance)"
	restart=0
	if [ "$code" != "200" ]; then
		errors=$((errors + 1))
	elif [ -n "$last_started" ] && [ "$started" != "$last_started" ]; then
		restart=1
		restarts=$((restarts + 1))
		r13_log "restart instance: $last_started -> $started"
	fi
	[ "$code" = "200" ] && last_started="$started"
	printf '%s,%s,%s,%s,%s,%s,%s,%s\n' "$(r13_ts)" "$n" "$code" "$t_total" "$instance" "$uptime_ms" "$started" "$restart" >>"$OUT"
	if [ $((n % 60)) -eq 0 ] || [ "$n" -eq 1 ]; then
		r13_log "$n/$total http=$code total=${t_total}s uptime_ms=$uptime_ms chyb=$errors restartů=$restarts"
	fi
	n=$((n + 1))
	[ "$n" -le "$total" ] && sleep "$INTERVAL"
done

r13_log "hotovo: požadavků=$total chyb=$errors restartů=$restarts"
awk -F, 'NR > 1 && $3 == 200 { print $4 }' "$OUT" | sort -n | awk '
	{ v[NR] = $1 }
	END {
		if (NR == 0) { print "žádné úspěšné odpovědi"; exit }
		printf "time_total s: min=%s p50=%s p95=%s max=%s\n", v[1], v[int(NR * 0.5) + 1], v[int(NR * 0.95) + 1 > NR ? NR : int(NR * 0.95) + 1], v[NR]
	}'
r13_log "CSV: $OUT"
