#!/usr/bin/env bash
# Test (a): 5 MB POST na /echo, lživý Content-Length, chunked tělo a sondy limitu těla.
set -euo pipefail
. "$(dirname "$0")/lib.sh"

usage() {
	cat <<'USAGE'
Použití: post-5mb.sh <base-url> [--size BAJTY] [--repeat N] [--probe-sizes "B1 B2"] [--out SOUBOR.csv]

Pošle náhodné tělo o velikosti --size (výchozí 5242880 = 5 MiB) N-krát na POST /echo,
změří čas a porovná vrácené received_bytes s odeslanou velikostí. Pak pošle:
  - tělo s Content-Length větším, než je skutečné tělo (ingress má buď čekat, nebo vrátit 400),
  - tělo s Content-Length menším, než je skutečné tělo,
  - tělo bez Content-Length (Transfer-Encoding: chunked); pokud echo vrátí content_length
    rovný velikosti, ingress tělo bufferuje a přepisuje hlavičku,
  - sondy velikosti podle --probe-sizes (výchozí "6291457 16777216"): 6 MiB + 1 B musí
    dostat 413 od echa (MaxBytesReader), 16 MiB ukáže, zda má ingress vlastní limit.

Výstup: řádky na stdout a CSV v --out (výchozí results/post-5mb-<čas>.csv).
Hlavička Expect: 100-continue je vypnutá, aby požadavek odpovídal prohlížeči.
USAGE
}

[ $# -ge 1 ] || { usage >&2; exit 2; }
case "$1" in -h | --help) usage; exit 0 ;; esac

BASE="$(r13_base_url "$1")"
shift
SIZE=5242880
REPEAT=3
PROBE_SIZES="6291457 16777216"
OUT=""
while [ $# -gt 0 ]; do
	case "$1" in
	--size) SIZE="$2"; shift 2 ;;
	--repeat) REPEAT="$2"; shift 2 ;;
	--probe-sizes) PROBE_SIZES="$2"; shift 2 ;;
	--out) OUT="$2"; shift 2 ;;
	-h | --help) usage; exit 0 ;;
	*) usage >&2; exit 2 ;;
	esac
done
[ -n "$OUT" ] || OUT="$(r13_results_dir)/post-5mb-$(date -u +%Y%m%dT%H%M%SZ).csv"

r13_need curl head sed awk mktemp
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

make_payload() {
	local bytes="$1" file="$2"
	head -c "$bytes" /dev/urandom >"$file"
}

post_case() {
	local label="$1" file="$2" expected="$3"
	shift 3
	local resp="$TMP/resp.json" stats code received clen tenc proto read_ms err time_total size_upload curl_exit
	: >"$resp"
	stats="$(r13_curl_json "$resp" --max-time 130 -X POST -H 'Content-Type: application/octet-stream' -H 'Expect:' "$@" --data-binary "@$file" "$BASE/echo")"
	code="${stats%% *}"
	time_total="$(printf '%s' "$stats" | awk '{print $6}')"
	size_upload="$(printf '%s' "$stats" | awk '{print $7}')"
	curl_exit="$(printf '%s' "$stats" | awk '{print $8}')"
	received="$(r13_json_num "$(cat "$resp")" received_bytes)"
	clen="$(r13_json_num "$(cat "$resp")" content_length)"
	read_ms="$(r13_json_num "$(cat "$resp")" read_ms)"
	proto="$(r13_json_str "$(cat "$resp")" proto)"
	err="$(r13_json_str "$(cat "$resp")" read_error)"
	tenc=""
	grep -q '"transfer_encoding"' "$resp" && tenc="chunked"
	local verdict="INFO"
	if [ -n "$expected" ]; then
		if [ "$code" = "200" ] && [ "$received" = "$expected" ]; then verdict="PASS"; else verdict="FAIL"; fi
	fi
	printf '%-28s http=%s curl_exit=%s upload=%s received=%s content_length=%s%s proto=%s read_ms=%s total_s=%s %s %s\n' \
		"$label" "$code" "$curl_exit" "$size_upload" "${received:--}" "${clen:--}" "${tenc:+ transfer_encoding=$tenc}" "${proto:--}" "${read_ms:--}" "$time_total" "$verdict" "${err:+read_error=$err}"
	printf '%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n' "$(r13_ts)" "$label" "$code" "$curl_exit" "$size_upload" "${received:-}" "${clen:-}" "${tenc:-}" "${proto:-}" "${read_ms:-}" "$time_total" "$verdict" >>"$OUT"
	if [ "$code" != "200" ] && [ -s "$resp" ] && ! grep -q '"received_bytes"' "$resp"; then
		printf '    odpověď nepochází z echa (ingress?): %s\n' "$(head -c 300 "$resp" | tr '\n' ' ')"
	fi
}

printf 'ts,case,http_code,curl_exit,size_upload,received_bytes,content_length,transfer_encoding,proto,read_ms,time_total_s,verdict\n' >"$OUT"
r13_log "cíl $BASE, velikost $SIZE B, opakování $REPEAT, CSV $OUT"

make_payload "$SIZE" "$TMP/payload.bin"
i=1
while [ "$i" -le "$REPEAT" ]; do
	post_case "post-${SIZE}B-$i" "$TMP/payload.bin" "$SIZE"
	i=$((i + 1))
done

post_case "content-length-too-big" "$TMP/payload.bin" "" -H "Content-Length: $((SIZE + 4096))"
post_case "content-length-too-small" "$TMP/payload.bin" "" -H "Content-Length: 4096"
post_case "chunked-no-content-length" "$TMP/payload.bin" "" -H 'Transfer-Encoding: chunked'

for probe in $PROBE_SIZES; do
	make_payload "$probe" "$TMP/probe.bin"
	post_case "probe-${probe}B" "$TMP/probe.bin" ""
done

r13_log "hotovo, CSV: $OUT"
