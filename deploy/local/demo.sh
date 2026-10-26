#!/bin/sh
# Laptop demo of the three acceptance scenarios: no server, no Zetaris, no
# model, no root. Run from the repo root:
#
#   deploy/local/demo.sh stopped   # toyd killed: restarted and verified
#   deploy/local/demo.sh badfix    # config and .good corrupted: undo, retry, escalate
#   deploy/local/demo.sh leak      # memory leak: no catalogue entry, escalate
#   deploy/local/demo.sh play      # no fault: ari runs until Ctrl+C, you break toyd yourself
set -eu

SCENARIO=${1:?usage: deploy/local/demo.sh stopped|badfix|leak|play}
case "$SCENARIO" in
stopped|badfix|leak) ;;
play) CYCLES=0 ;;
*) echo "usage: deploy/local/demo.sh stopped|badfix|leak|play" >&2; exit 2 ;;
esac

ROOT=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$ROOT"

PORT=${PORT:-8081}
CYCLES=${CYCLES:-15}
WORK=$(mktemp -d "${TMPDIR:-/tmp}/ari-local-demo.XXXXXX")
BIN=$WORK/bin
T=$WORK/target
D=$WORK/data
mkdir -p "$BIN" "$T" "$D"

INJECT=
cleanup() {
	[ -n "$INJECT" ] && kill "$INJECT" 2>/dev/null || true
	if [ -f "$T/toyd.pid" ]; then
		kill "$(cat "$T/toyd.pid")" 2>/dev/null || true
	fi
	rm -rf "$WORK"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

echo "building ari and toyd into $BIN"
go build -o "$BIN/ari" ./cmd/ari
go build -o "$BIN/toyd" ./cmd/toyd

printf 'port=%s\nstatus=ok\n' "$PORT" > "$T/toyd.conf"
cp "$T/toyd.conf" "$T/toyd.conf.good"
(cd "$T" && nohup "$BIN/toyd" -config "$T/toyd.conf" -pidfile "$T/toyd.pid" >>"$T/toyd.log" 2>&1 &)
sleep 1

# The baseline needs three clean cycles; inject once ari has run five.
inject() {
	until [ "$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["cycle"])' "$D/state.json" 2>/dev/null || echo 0)" -ge 5 ]; do
		sleep 0.3
	done
	case "$SCENARIO" in
	stopped) kill -9 "$(cat "$T/toyd.pid")" ;;
	badfix) printf '%%%% corrupted %%%%\n' | tee "$T/toyd.conf" > "$T/toyd.conf.good" ;;
	leak) kill -USR1 "$(cat "$T/toyd.pid")" ;;
	esac
}
if [ "$SCENARIO" = play ]; then
	echo "toyd runs in $T (pid in toyd.pid, config toyd.conf, backup toyd.conf.good)"
	echo "break it however you like from another terminal; $BIN/ari status -data $D shows the state; Ctrl+C stops"
else
	inject &
	INJECT=$!
fi

echo "skipping air gap preflight (laptop run; use the VM demo for the real check)"
rc=0
"$BIN/ari" run -preflight=false \
	-target "$T" -data "$D" -toyd "$BIN/toyd" \
	-catalogue catalogue/toyd.json -health "http://127.0.0.1:$PORT/health" \
	-cycles "$CYCLES" -interval 1s || rc=$?
kill "$INJECT" 2>/dev/null || true
INJECT=
echo
"$BIN/ari" report -data "$D"
exit "$rc"
