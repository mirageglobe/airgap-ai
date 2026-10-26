#!/bin/sh
# Rebuilds the demo servers from nothing. Run from a laptop with vultr-cli,
# the aws cli (profile and region set) and ssh, from the repo root:
#
#   deploy/recreate.sh nightshift            AWS arm64 box with headless Nightshift
#   deploy/recreate.sh ari NIGHTSHIFT_IP     Vultr server: Docker, Zetaris, ari, tunnel
#
# Run nightshift first; it prints the IP the second step needs. Two steps stay
# manual because they need your own logins: the Zetaris registry sign in and
# the NVIDIA API key. The script stops and says when.
#
# Reuses what deleting the servers leaves behind: the Vultr firewall group
# "ari ssh only" and ssh key "ari-vultr", the AWS key pair and security group
# "ari-nightshift". The Zetaris bundle is read from ZETARIS_ZIP.
set -eu

KEY=${KEY:-$HOME/.ssh/ari_vultr}
ZETARIS_ZIP=${ZETARIS_ZIP:-$HOME/zencargo/zetaris-platform.zip}
VULTR_PLAN=vhf-4c-16gb
VULTR_REGION=ams
VULTR_OS=2625 # Debian 13
GO_VERSION=1.27.3
GO_SHA256=65efbc80fdc1fa2cd2631cad5b481777a68e8725f34e89f50fc6ac714e379b40
NS_DEB=meterless-runtime_1.5.1-1_arm64.deb
NS_URL=https://meterless.ai/Nightshift/DGX
SSH="ssh -i $KEY -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -o BatchMode=yes -o ConnectTimeout=10"

wait_ssh() {
	i=0
	until $SSH "$1" true 2>/dev/null; do
		i=$((i + 1)); [ $i -gt 60 ] && { echo "no ssh to $1" >&2; exit 1; }
		sleep 10
	done
}

nightshift() {
	ami=$(aws ssm get-parameter --name /aws/service/canonical/ubuntu/server/24.04/stable/current/arm64/hvm/ebs-gp3/ami-id --query Parameter.Value --output text)
	sg=$(aws ec2 describe-security-groups --filters Name=group-name,Values=ari-nightshift --query 'SecurityGroups[0].GroupId' --output text)
	id=$(aws ec2 run-instances --image-id "$ami" --instance-type t4g.small --key-name ari-nightshift --security-group-ids "$sg" \
		--block-device-mappings 'DeviceName=/dev/sda1,Ebs={VolumeSize=20,VolumeType=gp3,DeleteOnTermination=true}' \
		--metadata-options HttpTokens=required \
		--tag-specifications 'ResourceType=instance,Tags=[{Key=Name,Value=ari-nightshift},{Key=project,Value=airgap-ai}]' \
		--query 'Instances[0].InstanceId' --output text)
	aws ec2 wait instance-running --instance-ids "$id"
	ip=$(aws ec2 describe-instances --instance-ids "$id" --query 'Reservations[0].Instances[0].PublicIpAddress' --output text)
	wait_ssh "ubuntu@$ip"
	# The runtime's CUDA dependencies serve local models only; the cloud lane
	# does not need them, so they are skipped (the media pack needs 12 GB).
	$SSH "ubuntu@$ip" "set -e; mkdir -p ns && cd ns
		curl -fsSL -o SHA256SUMS $NS_URL/SHA256SUMS && curl -fsSLO $NS_URL/$NS_DEB
		grep \" $NS_DEB\$\" SHA256SUMS | sha256sum -c -
		sudo METERLESS_USER=ubuntu dpkg --force-depends -i $NS_DEB >/dev/null
		sleep 5; systemctl is-active meterlessd"
	echo
	echo "Now enter your NVIDIA API key (build.nvidia.com); it is stored in Nightshift's key store:"
	$SSH -t "ubuntu@$ip" 'read -rsp "NVIDIA key: " K; echo; printf "{\"provider\":\"nvidia\",\"value\":\"%s\"}" "$K" | curl -s -X POST --unix-socket /run/meterless/runtime.sock http://d/admin/keys -H "content-type: application/json" --data-binary @-; echo'
	echo "nightshift box ready: $ip   next: deploy/recreate.sh ari $ip"
}

ari() {
	ns_ip=${1:?usage: deploy/recreate.sh ari NIGHTSHIFT_IP}
	[ -f "$ZETARIS_ZIP" ] || { echo "missing ZETARIS_ZIP: $ZETARIS_ZIP" >&2; exit 1; }
	key=$(vultr-cli ssh-key list -o json | python3 -c "import json,sys; print([k['id'] for k in json.load(sys.stdin)['ssh_keys'] if k['name']=='ari-vultr'][0])")
	fw=$(vultr-cli firewall group list -o json | python3 -c "import json,sys; print([g['id'] for g in json.load(sys.stdin)['firewall_groups'] if g['description']=='ari ssh only'][0])")
	id=$(vultr-cli instance create -r $VULTR_REGION -p $VULTR_PLAN --os $VULTR_OS -l ari --host ari -s "$key" --firewall-group "$fw" -o json |
		python3 -c "import json,sys; print(json.load(sys.stdin)['instance']['id'])")
	ip=0.0.0.0
	until [ "$ip" != 0.0.0.0 ] && vultr-cli instance get "$id" | grep -q "SERVER STATE.*ok"; do
		sleep 10
		ip=$(vultr-cli instance get "$id" -o json | python3 -c "import json,sys; print(json.load(sys.stdin)['instance']['main_ip'])")
	done
	wait_ssh "root@$ip"
	echo "server $ip: installing docker, go, tools"
	$SSH "root@$ip" "set -e; export DEBIAN_FRONTEND=noninteractive
		printf 'PasswordAuthentication no\nKbdInteractiveAuthentication no\nPermitRootLogin prohibit-password\n' > /etc/ssh/sshd_config.d/10-keys-only.conf && sshd -t && systemctl reload ssh
		apt-get update -qq && apt-get install -y -qq ca-certificates curl unzip asciinema >/dev/null
		install -m 0755 -d /etc/apt/keyrings
		curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc && chmod a+r /etc/apt/keyrings/docker.asc
		echo \"deb [arch=\$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian \$(. /etc/os-release && echo \$VERSION_CODENAME) stable\" > /etc/apt/sources.list.d/docker.list
		apt-get update -qq && apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin >/dev/null
		cd /tmp && curl -fsSLO https://go.dev/dl/go$GO_VERSION.linux-amd64.tar.gz
		echo '$GO_SHA256  go$GO_VERSION.linux-amd64.tar.gz' | sha256sum -c - && tar -C /usr/local -xzf go$GO_VERSION.linux-amd64.tar.gz && ln -sf /usr/local/go/bin/go /usr/local/bin/go
		id ari >/dev/null 2>&1 || useradd -m -s /bin/bash ari
		mkdir -p /opt/ari/bin /opt/ari/catalogue /var/lib/ari/data /var/lib/ari/target"
	echo "building ari from this checkout's HEAD"
	git archive --format=tar HEAD | $SSH "root@$ip" "set -e
		rm -rf /opt/ari/src && mkdir -p /opt/ari/src && tar -x -C /opt/ari/src && cd /opt/ari/src
		CGO_ENABLED=0 go build -o /opt/ari/bin/ari ./cmd/ari && CGO_ENABLED=0 go build -o /opt/ari/bin/toyd ./cmd/toyd
		cp catalogue/toyd.json /opt/ari/catalogue/ && install -m 755 deploy/vm/demo.sh /opt/ari/demo.sh
		ln -sf /opt/ari/bin/ari /usr/local/bin/ari && ln -sf /opt/ari/demo.sh /usr/local/bin/ari-demo
		printf 'port=8081\nstatus=ok\n' > /var/lib/ari/target/toyd.conf && cp /var/lib/ari/target/toyd.conf /var/lib/ari/target/toyd.conf.good
		chown -R ari:ari /var/lib/ari"
	scp -q -i "$KEY" -o IdentitiesOnly=yes "$ZETARIS_ZIP" "root@$ip:/opt/zetaris-platform.zip"
	$SSH "root@$ip" "cd /opt && unzip -q -o zetaris-platform.zip"
	echo
	echo "Now sign in to the Zetaris registry on the server, in another terminal:"
	echo "  ssh -i $KEY root@$ip"
	echo "  then paste the docker login command from the Zetaris Run locally page"
	printf "Press Enter when it says Login Succeeded... "; read -r _
	echo "starting Zetaris (first start takes 10 to 20 minutes)"
	$SSH "root@$ip" "set -e; cd /opt/zetaris-platform && ./preflight.sh && docker compose pull -q && docker compose up -d >/dev/null
		i=0; until ./verify.sh >/dev/null 2>&1; do i=\$((i+1)); [ \$i -gt 60 ] && { ./verify.sh; exit 1; }; sleep 20; done; echo zetaris ready"
	$SSH "root@$ip" "sh /opt/ari/src/deploy/vm/setup.sh"
	echo "wiring the Nightshift tunnel"
	pub=$($SSH "root@$ip" "test -f /root/.ssh/nightshift_tunnel || ssh-keygen -q -t ed25519 -N '' -C ari-nightshift-tunnel -f /root/.ssh/nightshift_tunnel; cat /root/.ssh/nightshift_tunnel.pub")
	$SSH "ubuntu@$ns_ip" "grep -q ari-nightshift-tunnel ~/.ssh/authorized_keys || echo 'restrict,port-forwarding,permitopen=\"127.0.0.1:8088\" $pub' >> ~/.ssh/authorized_keys"
	out=$(aws ec2 authorize-security-group-ingress --group-name ari-nightshift --protocol tcp --port 22 --cidr "$ip/32" 2>&1) ||
		case $out in *Duplicate*) ;; *) echo "$out" >&2; exit 1 ;; esac
	$SSH "root@$ip" "echo NIGHTSHIFT_HOST=ubuntu@$ns_ip > /etc/ari/nightshift-tunnel.conf
		install -m 644 /opt/ari/src/deploy/vm/nightshift-tunnel.service /etc/systemd/system/
		systemctl daemon-reload && systemctl enable --now nightshift-tunnel >/dev/null 2>&1"
	$SSH "ubuntu@$ns_ip" 'cat /var/lib/meterless/proxy-token' | $SSH "root@$ip" 'T=$(cat); [ -n "$T" ] || { echo "empty nightshift token" >&2; exit 1; }
		sed -i "/^METERLESS_/d" /etc/ari/ari.conf
		printf "METERLESS_BASE_URL=http://127.0.0.1:8088/v1\nMETERLESS_RUNTIME_TOKEN=%s\nMETERLESS_MODEL=meterless/auto\n" "$T" >> /etc/ari/ari.conf'
	$SSH "root@$ip" "runuser -u ari -- sh -c 'cd /var/lib/ari/target && nohup /opt/ari/bin/toyd -config toyd.conf -pidfile toyd.pid >>toyd.log 2>&1 &'
		systemctl start ari; sleep 20; ari status -data /var/lib/ari/data"
	echo "ari server ready: $ip   try: ssh -i $KEY root@$ip, then ari-demo stopped"
}

case "${1:-}" in
nightshift) nightshift ;;
ari) shift; ari "$@" ;;
*) sed -n '2,13p' "$0"; exit 2 ;;
esac
