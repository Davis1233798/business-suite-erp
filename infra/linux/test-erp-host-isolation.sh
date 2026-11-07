#!/usr/bin/env bash
set -Eeuo pipefail
fail(){ echo "FAIL: $*" >&2; exit 1; }
for user in erp-runtime erp-deploy; do getent passwd "$user" >/dev/null || fail "missing $user"; done
runtime_uid=$(id -u erp-runtime); deploy_uid=$(id -u erp-deploy)
[[ $runtime_uid != 0 && $deploy_uid != 0 && $runtime_uid != "$deploy_uid" ]] || fail 'accounts are not distinct non-root users'
[[ " $(id -nG erp-runtime) " != *' docker '* && " $(id -nG erp-deploy) " != *' docker '* ]] || fail 'ERP accounts joined rootful docker group'
if [[ -S /var/run/docker.sock ]] && runuser -u erp-runtime -- test -r /var/run/docker.sock; then fail 'runtime account can read the rootful Docker socket'; fi
if [[ -S /var/run/docker.sock ]] && runuser -u erp-deploy -- test -r /var/run/docker.sock; then fail 'deploy account can read the rootful Docker socket'; fi
[[ $(grep -c '^erp-runtime:' /etc/subuid) == 1 && $(grep '^erp-runtime:' /etc/subuid | cut -d: -f3) == 65536 ]] || fail 'runtime subuid mapping is not unique and 65536 wide'
[[ $(grep -c '^erp-runtime:' /etc/subgid) == 1 && $(grep '^erp-runtime:' /etc/subgid | cut -d: -f3) == 65536 ]] || fail 'runtime subgid mapping is not unique and 65536 wide'
for database in /etc/subuid /etc/subgid; do
  awk -F: -v target=erp-runtime '$1==target {s=$2;e=$2+$3-1;next} {other[++n]=$0} END{if (!s) exit 1; for(i=1;i<=n;i++){split(other[i],a,":");if(a[2]<=e && a[2]+a[3]-1>=s)exit 1}}' "$database" || fail "runtime range overlaps another entry in $database"
done
[[ $(getent passwd erp-runtime | cut -d: -f7) == /usr/sbin/nologin ]] || fail 'runtime shell is interactive'
[[ $(stat -c '%U:%a' /var/lib/erp-runtime) == erp-runtime:700 ]] || fail 'runtime home permissions invalid'
for f in /usr/local/lib/business-suite-erp/erp_gateway.py /usr/local/lib/business-suite-erp/erp_gateway_protocol.py /usr/local/lib/business-suite-erp/erp_image_release.py /usr/local/lib/business-suite-erp/erp_activate.py /usr/local/lib/business-suite-erp/erp_candidate.py; do [[ $(stat -c '%U:%a' "$f") == root:444 ]] || fail "installed program not immutable root-owned: $f"; done
[[ $(stat -c '%U:%a' /usr/local/lib/business-suite-erp) == root:755 ]] || fail 'ERP program directory ownership/mode invalid'
[[ $(stat -c '%U:%a' /etc/sudoers.d/erp-deploy) == root:440 ]] || fail 'sudoers ownership/mode invalid'
[[ $(stat -c '%U:%a' /var/lib/erp-deploy/.ssh/authorized_keys) == erp-deploy:600 ]] || fail 'deployment authorized_keys ownership/mode invalid'
[[ $(wc -l < /var/lib/erp-deploy/.ssh/authorized_keys) == 1 && $(cut -d' ' -f1 /var/lib/erp-deploy/.ssh/authorized_keys) == restrict ]] || fail 'deployment key is missing OpenSSH restrict option'
[[ $(sudo -n -l -U erp-deploy 2>&1 | grep -E '^    \(' | sed 's/^[[:space:]]*//') == '(erp-runtime) NOPASSWD: /usr/bin/python3 -Es /usr/local/lib/business-suite-erp/erp_gateway.py --runtime' ]] || fail 'sudo grant is not exactly scoped'
runtime_dir=/run/user/$runtime_uid
[[ -S $runtime_dir/docker.sock && $(stat -c '%U:%a' "$runtime_dir/docker.sock") == erp-runtime:600 ]] || fail 'rootless socket ownership/mode invalid'
security=$(runuser -u erp-runtime -- env -i HOME=/var/lib/erp-runtime XDG_RUNTIME_DIR="$runtime_dir" DOCKER_HOST="unix://$runtime_dir/docker.sock" docker info --format '{{json .SecurityOptions}}')
[[ $security == *'"name=rootless"'* ]] || fail 'Docker daemon is not rootless'
root=$(runuser -u erp-runtime -- env -i HOME=/var/lib/erp-runtime XDG_RUNTIME_DIR="$runtime_dir" DOCKER_HOST="unix://$runtime_dir/docker.sock" docker info --format '{{.DockerRootDir}}')
[[ $root == /var/lib/erp-runtime/* ]] || fail 'Docker data escaped ERP runtime home'
effective=$(sshd -T -C "user=erp-deploy,host=$(hostname -f),addr=127.0.0.1")
grep -Fq 'forcecommand /usr/bin/python3 -Es /usr/local/lib/business-suite-erp/erp_gateway.py --ssh' <<< "$effective" || fail 'SSH command is not forced'
grep -Fq 'authenticationmethods publickey' <<< "$effective" || fail 'SSH does not require public key'
grep -Fq 'disableforwarding yes' <<< "$effective" || fail 'SSH forwarding is not disabled'
[[ $(systemctl is-active docker.service) == active ]] || fail 'rootful Docker is not active'
aa-status --enabled || fail 'AppArmor is not enabled'
[[ -f /etc/apparmor.d/rootlesskit ]] && aa-status | grep -Fq rootlesskit || fail 'shared rootlesskit profile is not loaded'
echo 'PASS: ERP runtime uses isolated rootless Docker and exact deployment gateway policy.'
