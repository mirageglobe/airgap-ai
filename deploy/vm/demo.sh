#!/bin/sh
# Runs one demo scenario on the VM as root: sets up the toy target, starts
# toyd, injects a fault after a few clean cycles, runs ari, prints the report.
#
#   demo.sh stopped   Jimmy's test 1: toyd is killed
#   demo.sh badfix    test 2: config and its known good copy are corrupted
#   demo.sh leak      test 3: a memory leak no catalogue entry handles
set -eu

SCENARIO=${1:?usage: demo.sh stopped|badfix|leak}
CYCLES=${CYCLES:-15}
T=/var/lib/ari/target
D=/var/lib/ari/data
BIN=/opt/ari/bin
PORT=8081

. /etc/ari/evidence.conf
export ARI_EVIDENCE_DSN="postgres://ari:${ARI_PW}@127.0.0.1:5433/evidence?sslmode=disable"
export ZETARIS_BASE_URL=${ZETARIS_BASE_URL:-http://127.0.0.1:3000}
export ZETARIS_PASSWORD=${ZETARIS_PASSWORD:-Zetaris1@}

pkill -x toyd || true
rm -rf "$T" "$D"
mkdir -p "$T" "$D"
printf 'port=%s\nstatus=ok\n' "$PORT" > "$T/toyd.conf"
cp "$T/toyd.conf" "$T/toyd.conf.good"
chown -R ari:ari /var/lib/ari
runuser -u ari -- sh -c "cd $T && nohup $BIN/toyd -config $T/toyd.conf -pidfile $T/toyd.pid >>$T/toyd.log 2>&1 &"
sleep 1

# The baseline needs three clean cycles; inject after about five.
inject() {
	sleep 7
	case "$SCENARIO" in
	stopped) kill -9 "$(cat "$T/toyd.pid")" ;;
	badfix)
		printf '%%%% corrupted %%%%\n' | tee "$T/toyd.conf" > "$T/toyd.conf.good"
		chown ari:ari "$T/toyd.conf" "$T/toyd.conf.good" ;;
	leak) kill -USR1 "$(cat "$T/toyd.pid")" ;;
	*) echo "unknown scenario $SCENARIO" >&2; exit 2 ;;
	esac
}
inject &

runuser -u ari -p -- "$BIN/ari" run -target "$T" -data "$D" -toyd "$BIN/toyd" \
	-catalogue /opt/ari/catalogue/toyd.json -health "http://127.0.0.1:$PORT/health" \
	-cycles "$CYCLES" -interval 1s
wait
echo
runuser -u ari -- "$BIN/ari" report -data "$D"
