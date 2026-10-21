#!/usr/bin/env bash
# Test (e): výpis hlaviček z /echo pro určení důvěryhodné proxy a režimu NYLZO_PROXY_MODE=scaleway.
set -euo pipefail
. "$(dirname "$0")/lib.sh"

usage() {
	cat <<'USAGE'
Použití: headers.sh <base-url> [--my-ip IP | --no-ip-lookup] [--out SOUBOR.txt]

1. Zjistí veřejnou IP notebooku (dotaz na https://api.ipify.org, obejdeš přes --my-ip
   nebo vypneš přes --no-ip-lookup).
2. Pošle čistý POST /echo a vypíše remote_addr (adresa, ze které se kontejner připojuje:
   kandidát na NYLZO_TRUSTED_PROXIES), X-Real-IP, X-Forwarded-For, X-Envoy-External-Address,
   X-Forwarded-Proto, protokol a všechny hlavičky, které do kontejneru dorazily.
3. Pošle POST /echo s podvrženými hlavičkami (X-Real-IP, X-Forwarded-For,
   X-Envoy-External-Address, Forwarded = 198.51.100.77) a ukáže, co z nich ingress propustil.
4. Vyhodnotí: pokud X-Real-IP v podvrženém požadavku obsahuje veřejnou IP notebooku, a ne
   198.51.100.77, ingress hlavičku přepisuje a NYLZO_PROXY_MODE=scaleway může X-Real-IP věřit.
USAGE
}

[ $# -ge 1 ] || { usage >&2; exit 2; }
case "$1" in -h | --help) usage; exit 0 ;; esac

BASE="$(r13_base_url "$1")"
shift
MY_IP=""
LOOKUP=1
OUT=""
while [ $# -gt 0 ]; do
	case "$1" in
	--my-ip) MY_IP="$2"; LOOKUP=0; shift 2 ;;
	--no-ip-lookup) LOOKUP=0; shift ;;
	--out) OUT="$2"; shift 2 ;;
	-h | --help) usage; exit 0 ;;
	*) usage >&2; exit 2 ;;
	esac
done
[ -n "$OUT" ] || OUT="$(r13_results_dir)/headers-$(date -u +%Y%m%dT%H%M%SZ).txt"

r13_need curl sed awk mktemp
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
SPOOF="198.51.100.77"

if [ "$LOOKUP" -eq 1 ]; then
	MY_IP="$(curl -sS --max-time 10 https://api.ipify.org || true)"
	[ -n "$MY_IP" ] || r13_log "veřejnou IP se nepodařilo zjistit, verdikt bude neúplný"
fi
r13_log "cíl $BASE, veřejná IP notebooku: ${MY_IP:-neznámá}, výstup $OUT"

print_fields() {
	local body="$1"
	printf '  remote_addr=%s\n' "$(r13_json_str "$body" remote_addr)"
	printf '  x_real_ip=%s\n' "$(r13_json_str "$body" x_real_ip)"
	printf '  x_forwarded_for=%s\n' "$(r13_json_str "$body" x_forwarded_for)"
	printf '  x_envoy_external_address=%s\n' "$(r13_json_str "$body" x_envoy_external_address)"
	printf '  proto=%s tls=%s host=%s\n' "$(r13_json_str "$body" proto)" "$(printf '%s' "$body" | sed -n -E 's/.*"tls":(true|false).*/\1/p')" "$(r13_json_str "$body" host)"
	printf '  headers=%s\n' "$(printf '%s' "$body" | sed -n 's/.*"headers":{\(.*\)},"instance".*/\1/p')"
}

{
	printf '=== čistý požadavek (%s) ===\n' "$(r13_ts)"
	clean="$(curl -sS --max-time 30 -X POST -H 'Content-Type: text/plain' --data 'ping' "$BASE/echo")"
	print_fields "$clean"
	printf '%s\n\n' "$clean" >"$TMP/clean.json"

	printf '=== podvržené hlavičky = %s (%s) ===\n' "$SPOOF" "$(r13_ts)"
	spoofed="$(curl -sS --max-time 30 -X POST -H 'Content-Type: text/plain' \
		-H "X-Real-IP: $SPOOF" -H "X-Forwarded-For: $SPOOF" -H "X-Envoy-External-Address: $SPOOF" -H "Forwarded: for=$SPOOF" \
		--data 'ping' "$BASE/echo")"
	print_fields "$spoofed"
	printf '%s\n' "$spoofed" >"$TMP/spoofed.json"

	printf '\n=== vyhodnocení ===\n'
	clean_rip="$(r13_json_str "$clean" x_real_ip)"
	spoof_rip="$(r13_json_str "$spoofed" x_real_ip)"
	spoof_xff="$(r13_json_str "$spoofed" x_forwarded_for)"
	spoof_envoy="$(r13_json_str "$spoofed" x_envoy_external_address)"
	remote="$(r13_json_str "$clean" remote_addr)"
	printf 'adresa spojení (kandidát NYLZO_TRUSTED_PROXIES): %s\n' "${remote%:*}"
	if [ -n "$spoof_rip" ] && [ "$spoof_rip" != "$SPOOF" ]; then
		printf 'X-Real-IP: ingress přepisuje (podvržená hodnota zmizela, dorazilo %s)' "$spoof_rip"
		if [ -n "$MY_IP" ] && [ "$spoof_rip" = "$MY_IP" ]; then
			printf ' = veřejná IP notebooku -> PASS, NYLZO_PROXY_MODE=scaleway může číst X-Real-IP\n'
		elif [ -n "$MY_IP" ]; then
			printf ' != veřejná IP notebooku %s -> X-Real-IP nese adresu proxy, ne klienta; ověř ručně\n' "$MY_IP"
		else
			printf ' (veřejná IP neznámá, porovnej ručně)\n'
		fi
	elif [ "$spoof_rip" = "$SPOOF" ]; then
		printf 'X-Real-IP: FAIL, podvržená hodnota prošla; X-Real-IP nelze věřit\n'
	else
		printf 'X-Real-IP: ingress hlavičku neposílá (čistý požadavek: %s)\n' "${clean_rip:-prázdné}"
	fi
	case "$spoof_xff" in
	*"$SPOOF"*) printf 'X-Forwarded-For: podvržená hodnota zůstala v řetězci (%s); použitelná jen s pravidlem „nejpravější mimo důvěryhodné proxy“\n' "$spoof_xff" ;;
	"") printf 'X-Forwarded-For: nedorazila\n' ;;
	*) printf 'X-Forwarded-For: ingress přepsal na %s\n' "$spoof_xff" ;;
	esac
	if [ "$spoof_envoy" = "$SPOOF" ]; then
		printf 'X-Envoy-External-Address: podvržená hodnota prošla (hlavička je podle dokumentace zastaralá, nepoužívat)\n'
	else
		printf 'X-Envoy-External-Address: %s\n' "${spoof_envoy:-nedorazila}"
	fi
	printf 'protokol do kontejneru: %s, TLS v kontejneru: %s\n' "$(r13_json_str "$clean" proto)" "$(printf '%s' "$clean" | sed -n -E 's/.*"tls":(true|false).*/\1/p')"
} | tee "$OUT"
