#!/usr/bin/env bash
# Fail-closed ERP rootless runtime host provisioning. Intended for explicit operator use.
set -Eeuo pipefail
umask 077

readonly STATE=/etc/erp-oci-provisioner
readonly MARKER=$STATE/owner
readonly RUNTIME=erp-runtime DEPLOY=erp-deploy
readonly HOME_RUNTIME=/var/lib/erp-runtime HOME_DEPLOY=/var/lib/erp-deploy
readonly LIB=/usr/local/lib/business-suite-erp
readonly GATEWAY=$LIB/erp_gateway.py
readonly ACTIVATE=$LIB/erp_activate.py
readonly SUDOERS=/etc/sudoers.d/erp-deploy
readonly SSHD=/etc/ssh/sshd_config
readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
readonly REPO_ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"
MODE= KEY_FILE=
fail(){ printf 'ERROR: %s\n' "$*" >&2; exit 1; }
usage(){ echo "Usage: sudo bash $0 --check|--apply --deploy-public-key-file PATH"; }
while (($#)); do
  case "$1" in
    --check|--apply) [[ -z $MODE ]] || { usage >&2; exit 2; }; MODE=${1#--}; shift ;;
    --deploy-public-key-file) (($# >= 2)) || { usage >&2; exit 2; }; KEY_FILE=$2; shift 2 ;;
    *) usage >&2; exit 2 ;;
  esac
done
[[ $EUID == 0 ]] || fail 'must run as root'
[[ -n $MODE && -n $KEY_FILE && -f $KEY_FILE && ! -L $KEY_FILE && -r $KEY_FILE ]] || { usage >&2; exit 2; }
[[ -r /etc/os-release ]] || fail 'cannot identify host OS'
# shellcheck disable=SC1091
source /etc/os-release
[[ ${ID:-} == ubuntu && ${VERSION_ID:-} == 24.04 && $(dpkg --print-architecture) == arm64 && $(uname -m) == aarch64 ]] || fail 'requires Ubuntu 24.04 ARM64'
[[ -d /run/systemd/system && -r /sys/fs/cgroup/cgroup.controllers ]] || fail 'requires systemd with cgroup v2'
[[ $(wc -l < "$KEY_FILE") == 1 ]] || fail 'operator public key file must contain exactly one line'
read -r key_type key_blob _ < "$KEY_FILE"
[[ $key_type =~ ^(ssh-ed25519|sk-ssh-ed25519@openssh.com|ecdsa-sha2-nistp256|ssh-rsa)$ && $key_blob =~ ^[A-Za-z0-9+/]+=*$ ]] || fail 'invalid OpenSSH public key'
for cmd in /usr/bin/python3 /usr/bin/docker /usr/bin/systemctl /usr/bin/loginctl /usr/sbin/sshd /usr/sbin/visudo /usr/sbin/runuser /usr/bin/newuidmap /usr/bin/rootlesskit; do [[ -x $cmd ]] || fail "missing prerequisite $cmd"; done
[[ -d /usr/local/libexec/mes-rootless && ! -L /usr/local/libexec/mes-rootless ]] || fail 'pinned rootless launcher directory unavailable'
[[ -f /usr/local/libexec/mes-rootless/dockerd-rootless.sh && -f /usr/local/libexec/mes-rootless/dockerd-rootless-setuptool.sh ]] || fail 'pinned rootless launchers unavailable'
[[ $(stat -c '%u:%g:%a' /usr/local/libexec/mes-rootless) == 0:0:755 ]] || fail 'rootless launcher directory must be immutable root-owned'
for launcher in /usr/local/libexec/mes-rootless/dockerd-rootless.sh /usr/local/libexec/mes-rootless/dockerd-rootless-setuptool.sh; do [[ $(stat -c '%u:%g:%a' "$launcher") == 0:0:755 ]] || fail 'rootless launchers must be immutable root-owned'; done
[[ -f $SSHD && ! -L $SSHD && $(stat -c '%u:%g' "$SSHD") == 0:0 ]] || fail 'unsafe sshd_config'
sshd -t || fail 'existing SSH configuration invalid'
if [[ -e $STATE ]]; then
  [[ -d $STATE && ! -L $STATE && $(stat -c '%u:%g:%a' "$STATE") == 0:0:750 && -f $MARKER && ! -L $MARKER && $(cat "$MARKER") == business-suite-erp/host-provisioner/v1 ]] || fail 'foreign or incomplete ERP provisioner state'
  [[ -f $GATEWAY && $(stat -c '%u:%g:%a' "$GATEWAY") == 0:0:444 ]] || fail 'managed ERP installation is incomplete'
  for file in erp_gateway.py erp_gateway_protocol.py erp_image_release.py erp_activate.py erp_candidate.py; do cmp -s "$LIB/$file" "$REPO_ROOT/infra/linux/$file" || fail "installed ERP program differs from source: $file"; done
  if [[ $MODE == apply ]]; then bash "$SCRIPT_DIR/test-erp-host-isolation.sh"; echo 'ERP host already provisioned; no changes made.'; exit 0; fi
else
  for p in "$HOME_RUNTIME" "$HOME_DEPLOY" "$LIB" "$SUDOERS"; do [[ ! -e $p && ! -L $p ]] || fail "refusing to adopt $p"; done
  for u in "$RUNTIME" "$DEPLOY"; do ! getent passwd "$u" && ! getent group "$u" || fail "refusing to adopt account/group $u"; done
  ! grep -Eq '^erp-runtime:' /etc/subuid /etc/subgid 2>/dev/null || fail 'refusing to adopt existing ERP subordinate ID mappings'
  ! grep -q '^Match User erp-deploy$' "$SSHD" || fail 'existing erp-deploy SSH block found'
  for p in /etc/subuid /etc/subgid; do [[ ! -e $p ]] || { [[ -f $p && ! -L $p && $(stat -c %u:%g "$p") == 0:0 ]] || fail "unsafe $p"; }; done
fi

if [[ $MODE == check ]]; then
  [[ -d $STATE ]] || fail 'ERP runtime is not provisioned'
  bash "$SCRIPT_DIR/test-erp-host-isolation.sh"
  exit 0
fi

install -d -o root -g root -m 0750 "$STATE"
[[ -e /etc/subuid ]] || install -o root -g root -m 0644 /dev/null /etc/subuid
[[ -e /etc/subgid ]] || install -o root -g root -m 0644 /dev/null /etc/subgid
uid=470
while getent passwd "$uid" >/dev/null || getent passwd "$((uid+1))" >/dev/null || getent group "$uid" >/dev/null || getent group "$((uid+1))" >/dev/null; do uid=$((uid+1)); done
sub=300000
while awk -F: -v s="$sub" -v e="$((sub+65535))" '$2<=e && $2+$3-1>=s {found=1} END{exit !found}' /etc/subuid /etc/subgid 2>/dev/null; do sub=$((sub+65536)); done
(( uid < 60000 && sub+65535 < 4294967295 )) || fail 'no free uid/subuid ranges'
groupadd --system --gid "$uid" "$RUNTIME"; useradd --system --uid "$uid" --gid "$RUNTIME" --home-dir "$HOME_RUNTIME" --create-home --shell /usr/sbin/nologin "$RUNTIME"
groupadd --system --gid "$((uid+1))" "$DEPLOY"; useradd --system --uid "$((uid+1))" --gid "$DEPLOY" --home-dir "$HOME_DEPLOY" --create-home --shell /bin/bash "$DEPLOY"
passwd -l "$RUNTIME" >/dev/null; passwd -l "$DEPLOY" >/dev/null
chmod 0700 "$HOME_RUNTIME" "$HOME_DEPLOY"
printf '%s:%s:65536\n' "$RUNTIME" "$sub" >> /etc/subuid; printf '%s:%s:65536\n' "$RUNTIME" "$sub" >> /etc/subgid
chown root:root /etc/subuid /etc/subgid; chmod 0644 /etc/subuid /etc/subgid
install -d -o root -g root -m 0755 "$LIB"
install -o root -g root -m 0444 "$REPO_ROOT/infra/linux/erp_gateway.py" "$GATEWAY"
install -o root -g root -m 0444 "$REPO_ROOT/infra/linux/erp_gateway_protocol.py" "$LIB/erp_gateway_protocol.py"
install -o root -g root -m 0444 "$REPO_ROOT/infra/linux/erp_image_release.py" "$LIB/erp_image_release.py"
install -o root -g root -m 0444 "$REPO_ROOT/infra/linux/erp_activate.py" "$ACTIVATE"
install -o root -g root -m 0444 "$REPO_ROOT/infra/linux/erp_candidate.py" "$LIB/erp_candidate.py"
install -o root -g root -m 0440 "$REPO_ROOT/infra/linux/erp-deploy.sudoers" "$SUDOERS"
visudo -cf "$SUDOERS" >/dev/null
install -d -o root -g root -m 0700 "$HOME_DEPLOY/.ssh"
printf 'restrict %s %s\n' "$key_type" "$key_blob" > "$HOME_DEPLOY/.ssh/authorized_keys"
chown -R "$DEPLOY:$DEPLOY" "$HOME_DEPLOY/.ssh"; chmod 0700 "$HOME_DEPLOY/.ssh"; chmod 0600 "$HOME_DEPLOY/.ssh/authorized_keys"
cp -a "$SSHD" "$STATE/sshd_config.before"; chmod 0600 "$STATE/sshd_config.before"
printf '\n' >> "$SSHD"; cat "$REPO_ROOT/infra/linux/sshd-erp-deploy.conf" >> "$SSHD"
if ! sshd -t; then cp -a "$STATE/sshd_config.before" "$SSHD"; fail 'sshd rejected ERP policy; original config restored'; fi
effective=$(sshd -T -C "user=$DEPLOY,host=$(hostname -f),addr=127.0.0.1")
if ! grep -Fq 'forcecommand /usr/bin/python3 -Es /usr/local/lib/business-suite-erp/erp_gateway.py --ssh' <<< "$effective"; then cp -a "$STATE/sshd_config.before" "$SSHD"; fail 'effective SSH policy lacks fixed gateway; original config restored'; fi
loginctl enable-linger "$RUNTIME"
runtime_uid=$(id -u "$RUNTIME")
for _ in $(seq 1 30); do [[ -S /run/user/$runtime_uid/bus ]] && break; sleep 1; done
[[ -S /run/user/$runtime_uid/bus ]] || fail 'runtime user bus unavailable'
runuser -u "$RUNTIME" -- env -i HOME="$HOME_RUNTIME" PATH=/usr/bin:/bin install -d -m 0700 "$HOME_RUNTIME/.config/systemd/user/docker.service.d"
runuser -u "$RUNTIME" -- env -i HOME="$HOME_RUNTIME" PATH=/usr/bin:/bin sh -c 'umask 077; cat > "$HOME/.config/systemd/user/docker.service.d/erp-environment.conf"' <<EOF
[Service]
Environment=HOME=$HOME_RUNTIME
Environment=USER=$RUNTIME
Environment=LOGNAME=$RUNTIME
Environment=XDG_RUNTIME_DIR=/run/user/$runtime_uid
Environment=XDG_DATA_HOME=$HOME_RUNTIME/.local/share
Environment=DOCKER_CONFIG=$HOME_RUNTIME/.docker
UnsetEnvironment=DOCKER_HOST DOCKER_CONTEXT DOCKER_TLS_VERIFY DOCKER_CERT_PATH DOCKER_API_VERSION
ExecStartPost=/usr/bin/chmod 0600 /run/user/$runtime_uid/docker.sock
EOF
runuser -u "$RUNTIME" -- env -i HOME="$HOME_RUNTIME" USER="$RUNTIME" LOGNAME="$RUNTIME" XDG_RUNTIME_DIR="/run/user/$runtime_uid" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$runtime_uid/bus" PATH=/usr/local/libexec/mes-rootless:/usr/bin:/bin /usr/local/libexec/mes-rootless/dockerd-rootless-setuptool.sh install --force
for _ in $(seq 1 60); do [[ -S /run/user/$runtime_uid/docker.sock ]] && break; sleep 2; done
[[ -S /run/user/$runtime_uid/docker.sock && $(stat -c '%U:%a' /run/user/$runtime_uid/docker.sock) == "$RUNTIME:600" ]] || fail 'rootless Docker socket unavailable or unsafe'
[[ $(systemctl is-active docker.service) == active ]] || fail 'rootful Docker service was disrupted'
if systemctl is-active --quiet ssh.service && ! systemctl reload ssh.service; then cp -a "$STATE/sshd_config.before" "$SSHD"; systemctl reload ssh.service || true; fail 'SSH daemon did not reload ERP policy; original config restored'; fi
printf '%s\n' business-suite-erp/host-provisioner/v1 > "$MARKER"; chown root:root "$MARKER"; chmod 0444 "$MARKER"
bash "$SCRIPT_DIR/test-erp-host-isolation.sh"
echo 'ERP rootless host provisioned. No ERP container was deployed.'
