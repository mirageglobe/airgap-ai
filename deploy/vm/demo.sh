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

systemctl stop ari 2>/dev/null || true
pkill -x toyd || true
rm -rf "$T" "$D"
mkdir -p "$T" "$D"
printf 'port=%s\nstatus=ok\n' "$PORT" > "$T/toyd.conf"
cp "$T/toyd.conf" "$T/toyd.conf.good"
chown -R ari:ari /var/lib/ari
runuser -u ari -- sh -c "cd $T && nohup $BIN/toyd -config $T/toyd.conf -pidfile $T/toyd.pid >>$T/toyd.log 2>&1 &"
sleep 1

# The baseline needs three clean cycles; inject once ari has run five.
inject() {
	until [ "$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["cycle"])' "$D/state.json" 2>/dev/null || echo 0)" -ge 5 ]; do
		sleep 0.3
	done
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

# Same sandbox and air gap as ari.service.
systemd-run --quiet --wait --pipe --collect --unit "ari-demo-$SCENARIO" \
	-p User=ari -p Group=ari -p EnvironmentFile=/etc/ari/ari.conf \
	-p IPAddressDeny=any -p IPAddressAllow=localhost \
	-p NoNewPrivileges=yes -p ProtectSystem=strict -p ReadWritePaths=/var/lib/ari \
	-p ProtectHome=yes -p PrivateTmp=yes -p KillMode=process \
	"$BIN/ari" run -target "$T" -data "$D" -toyd "$BIN/toyd" \
	-catalogue /opt/ari/catalogue/toyd.json -health "http://127.0.0.1:$PORT/health" \
	-cycles "$CYCLES" -interval 1s
wait
echo
runuser -u ari -- "$BIN/ari" report -data "$D"
