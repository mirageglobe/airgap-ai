#!/bin/sh
# Run on the Mac that runs Meterless Nightshift. It makes Nightshift answer on
# the server's own 127.0.0.1:8088, so ari reaches it without any route out of
# the server: the air gap stays IPAddressAllow=localhost.
#
#   deploy/mac/nightshift-tunnel.sh root@SERVER [ssh key]
#
# Then on the server, in /etc/ari/ari.conf:
#   METERLESS_BASE_URL=http://127.0.0.1:8088/v1
#   METERLESS_RUNTIME_TOKEN=<Nightshift runtime token>
#   METERLESS_MODEL=meterless/auto
# and restart ari: systemctl restart ari
set -eu

SERVER=${1:?usage: nightshift-tunnel.sh root@SERVER [ssh key]}
KEY=${2:-$HOME/.ssh/ari_vultr}

if ! curl -s -o /dev/null --max-time 3 http://127.0.0.1:8088/; then
	echo "Nightshift is not answering on 127.0.0.1:8088 on this Mac; start it first" >&2
	exit 1
fi
echo "tunnel up: the server's 127.0.0.1:8088 is this Mac's Nightshift; ctrl c to stop"
exec ssh -i "$KEY" -o IdentitiesOnly=yes -o ExitOnForwardFailure=yes -o ServerAliveInterval=30 \
	-N -R 127.0.0.1:8088:127.0.0.1:8088 "$SERVER"
