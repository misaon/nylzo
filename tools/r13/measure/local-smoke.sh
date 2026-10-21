#!/usr/bin/env bash
# Lokální kouřový test harnessu: sestaví image echa, spustí ho v Dockeru a prožene skripty.
set -euo pipefail
. "$(dirname "$0")/lib.sh"

usage() {
	cat <<'USAGE'
Použití: local-smoke.sh [--port 18080] [--keep]

Sestaví tools/r13/echo pro linux/amd64, spustí kontejner na 127.0.0.1:<port>, počká na
/healthz a spustí post-5mb.sh, headers.sh --no-ip-lookup a cold-start.sh --idle-minutes 0.
Kontejner po skončení zastaví (--keep ho nechá běžet). Na Apple Silicon běží amd64 image
pod emulací, takže časy nic neříkají o Scaleway; jde jen o funkčnost harnessu.
USAGE
}

PORT=18080
KEEP=0
while [ $# -gt 0 ]; do
	case "$1" in
	--port) PORT="$2"; shift 2 ;;
	--keep) KEEP=1; shift ;;
	-h | --help) usage; exit 0 ;;
	*) usage >&2; exit 2 ;;
	esac
done

r13_need docker curl
DIR="$(cd "$(dirname "$0")" && pwd)"
IMAGE="r13-echo:local"
NAME="r13-echo-smoke"

docker build --platform linux/amd64 -t "$IMAGE" "$DIR/../echo"
docker rm -f "$NAME" >/dev/null 2>&1 || true
docker run -d --rm --name "$NAME" --platform linux/amd64 -p "127.0.0.1:$PORT:8080" "$IMAGE" >/dev/null
if [ "$KEEP" -eq 0 ]; then
	trap 'docker rm -f "$NAME" >/dev/null 2>&1 || true' EXIT
fi

BASE="http://127.0.0.1:$PORT"
i=0
until curl -fsS --max-time 2 "$BASE/healthz" >/dev/null 2>&1; do
	i=$((i + 1))
	[ "$i" -lt 30 ] || r13_die "kontejner neodpovídá na $BASE/healthz"
	sleep 1
done
r13_log "echo běží na $BASE"

"$DIR/post-5mb.sh" "$BASE" --repeat 1 --probe-sizes "6291457"
"$DIR/headers.sh" "$BASE" --no-ip-lookup
"$DIR/cold-start.sh" "$BASE" --idle-minutes 0
r13_log "kouřový test prošel"
