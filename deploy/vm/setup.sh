#!/bin/sh
# One time setup on the VM, as root, after Zetaris is running from
# /opt/zetaris-platform and ari is built into /opt/ari. Safe to run again.
#
# Creates the evidence db that ari writes and Zetaris reads, registers it in
# Zetaris, writes /etc/ari/ari.conf, and installs the ari service and banner.
set -eu

ZETARIS_URL=${ZETARIS_URL:-http://127.0.0.1:3000}
ZETARIS_USER=${ZETARIS_USER:-admin@zetaris.com}
ZETARIS_PASSWORD=${ZETARIS_PASSWORD:-Zetaris1@}
HERE=$(cd "$(dirname "$0")" && pwd)

id ari >/dev/null 2>&1 || useradd -m -s /bin/bash ari
mkdir -p /var/lib/ari/data /var/lib/ari/target /etc/ari
chown -R ari:ari /var/lib/ari
chmod 700 /etc/ari

if [ ! -f /etc/ari/evidence.conf ]; then
	umask 077
	printf 'ARI_PW=%s\nZETARIS_RO_PW=%s\n' "$(openssl rand -hex 24)" "$(openssl rand -hex 24)" > /etc/ari/evidence.conf
	umask 022
fi
. /etc/ari/evidence.conf

echo "== evidence db"
docker inspect ari-evidence >/dev/null 2>&1 || docker run -d --name ari-evidence --restart unless-stopped \
	--network zetaris_default -p 127.0.0.1:5433:5432 -v ari-evidence-data:/var/lib/postgresql/data \
	-e POSTGRES_USER=ari -e POSTGRES_PASSWORD="$ARI_PW" -e POSTGRES_DB=evidence postgres:16-alpine >/dev/null
i=0
until docker exec ari-evidence pg_isready -U ari -d evidence >/dev/null 2>&1; do
	i=$((i + 1)); [ $i -gt 60 ] && { echo "evidence db did not start" >&2; exit 1; }
	sleep 1
done
docker exec -i ari-evidence psql -q -U ari -d evidence -v ON_ERROR_STOP=1 -v ro_pw="$ZETARIS_RO_PW" <<'SQL'
CREATE TABLE IF NOT EXISTS evidence_rows (
  run    text NOT NULL,
  id     bigint NOT NULL,
  cycle  int NOT NULL,
  kind   text NOT NULL,
  data   text NOT NULL,
  ts     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (run, id)
);
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'zetaris_ro') THEN
    CREATE ROLE zetaris_ro LOGIN;
  END IF;
END $$;
ALTER ROLE zetaris_ro PASSWORD :'ro_pw';
GRANT CONNECT ON DATABASE evidence TO zetaris_ro;
GRANT USAGE ON SCHEMA public TO zetaris_ro;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO zetaris_ro;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO zetaris_ro;
SQL

echo "== zetaris datasource"
TOKEN=$(curl -sf -X POST "$ZETARIS_URL/api/auth/login" -H 'Content-Type: application/json' \
	-d "$(printf '{"username":"%s","password":"%s"}' "$ZETARIS_USER" "$ZETARIS_PASSWORD")" |
	python3 -c 'import json,sys; print(json.load(sys.stdin)["idToken"])')
zsql() {
	python3 -c 'import json,sys; print(json.dumps({"sql": sys.argv[1], "limit": 10}))' "$1" |
		curl -sf -X POST "$ZETARIS_URL/api/proxy/sql-editor/sqls/run-query" -H 'Content-Type: application/json' \
			-H "Authorization: Bearer $TOKEN" -H 'X-Org-ID: 1' -H "X-Request-ID: $(cat /proc/sys/kernel/random/uuid)" \
			--data-binary @-
}
if ! zsql "SHOW DATASOURCES" | grep -q '"ari_evidence"'; then
	zsql "CREATE DATASOURCE ari_evidence DESCRIBE BY \"ari evidence\" OPTIONS (jdbcdriver \"org.postgresql.Driver\", jdbcurl \"jdbc:postgresql://ari-evidence:5432/evidence\", username \"zetaris_ro\", password \"$ZETARIS_RO_PW\", fetchsize \"1000\")" >/dev/null
fi
zsql 'REGISTER DATASOURCE TABLE "evidence_rows" FROM ari_evidence' >/dev/null || true
zsql "SELECT count(*) AS n FROM ari_evidence.evidence_rows" | grep -q '"n"' && echo "zetaris reads ari_evidence.evidence_rows"

echo "== /etc/ari/ari.conf"
umask 077
cat > /etc/ari/ari.conf <<EOF
ARI_EVIDENCE_DSN=postgres://ari:${ARI_PW}@127.0.0.1:5433/evidence?sslmode=disable
ZETARIS_BASE_URL=${ZETARIS_URL}
ZETARIS_USERNAME=${ZETARIS_USER}
ZETARIS_PASSWORD=${ZETARIS_PASSWORD}
EOF
umask 022

echo "== service and banner"
install -m 644 "$HERE/ari.service" /etc/systemd/system/ari.service
install -m 755 "$HERE/motd.sh" /etc/update-motd.d/90-ari
systemctl daemon-reload
echo "done: start with systemctl start ari; watch with ari watch -data /var/lib/ari/data"
