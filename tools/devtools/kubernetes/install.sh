#!/usr/bin/env bash
set -euo pipefail

VERSION=1.36.5
ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
PRIVATE="$ROOT/.run/kubernetes/cluster"
umask 077
mkdir -p "$PRIVATE"
chmod 700 "$PRIVATE"
test "$(uname -m)" = x86_64 || { echo '安装仅支持 amd64。' >&2; exit 2; }
test -d /run/systemd/system || { echo '必须先启用 WSL systemd。' >&2; exit 2; }
test "$(stat -fc %T /sys/fs/cgroup)" = cgroup2fs || { echo '必须使用 cgroup v2。' >&2; exit 2; }
python3 "$ROOT/tools/devtools/kubernetes/cluster.py" routes
finish_install() {
    sudo -n install -o "$(id -u)" -g "$(id -g)" -m 600 /etc/kubernetes/admin.conf "$PRIVATE/admin.conf"
    export KUBECONFIG="$PRIVATE/admin.conf"
    python3 - "$ROOT" <<'PYCODE'
import json, subprocess, sys
node=json.loads(subprocess.check_output(['kubectl','get','node','gopulse-wsl','-o','json']))
taints=[t for t in node['spec'].get('taints',[]) if t['key'] != 'node-role.kubernetes.io/control-plane']
subprocess.run(['kubectl','patch','node','gopulse-wsl','--type=merge','-p',json.dumps({'spec':{'taints':taints}})],check=True)
# 单节点不需要保留加入令牌。令牌内容不输出到终端。
rows=subprocess.check_output(['sudo','-n','kubeadm','token','list'],text=True).splitlines()[1:]
for row in rows:
    subprocess.run(['sudo','-n','kubeadm','token','delete',row.split()[0]],check=True,stdout=subprocess.DEVNULL)
PYCODE
    python3 "$ROOT/tools/devtools/kubernetes/cluster.py" addons
    kubectl wait node/gopulse-wsl --for=condition=Ready --timeout=300s
    kubectl -n kube-system rollout status deployment/coredns --timeout=300s
    python3 "$ROOT/tools/devtools/kubernetes/cluster.py" verify
}
if sudo -n test -f /etc/kubernetes/admin.conf; then
    test -f "$PRIVATE/owner" || { echo '已有集群不属于本工作区，停止安装。' >&2; exit 2; }
    test "$(cat "$PRIVATE/owner")" = "$ROOT" || { echo '集群属于其他工作区，停止安装。' >&2; exit 2; }
    finish_install
    exit 0
fi
test ! -f "$PRIVATE/owner" || { echo '上次初始化未完成。请检查日志并手动恢复；禁止自动重置。' >&2; exit 2; }
sudo -n true
sudo -n apt-get update
sudo -n apt-get install -y ca-certificates curl gpg conntrack socat iptables
sudo -n install -d -m 755 /etc/apt/keyrings
curl -fsSL https://pkgs.k8s.io/core:/stable:/v1.36/deb/Release.key > "$PRIVATE/Release.key"
gpg --dearmor --yes --output "$PRIVATE/kubernetes.gpg" "$PRIVATE/Release.key"
sudo -n install -m 644 "$PRIVATE/kubernetes.gpg" /etc/apt/keyrings/gopulse-kubernetes.gpg
echo 'deb [signed-by=/etc/apt/keyrings/gopulse-kubernetes.gpg] https://pkgs.k8s.io/core:/stable:/v1.36/deb/ /' > "$PRIVATE/kubernetes.list"
sudo -n install -m 644 "$PRIVATE/kubernetes.list" /etc/apt/sources.list.d/gopulse-kubernetes.list
sudo -n apt-get update
sudo -n apt-get -o Acquire::https::Proxy::prod-cdn.packages.k8s.io=DIRECT install -y "kubeadm=$VERSION-1.1" "kubelet=$VERSION-1.1" "kubectl=$VERSION-1.1"
sudo -n apt-mark hold kubeadm kubelet kubectl
command -v containerd >/dev/null
containerd --version | rg ' v2\.' >/dev/null || { echo '专用运行时要求 containerd 2.x。' >&2; exit 2; }
sudo -n modprobe overlay
sudo -n modprobe br_netfilter
printf '%s\n' overlay br_netfilter > "$PRIVATE/modules.conf"
sudo -n install -m 644 "$PRIVATE/modules.conf" /etc/modules-load.d/gopulse-kubernetes.conf
printf '%s\n' 'net.bridge.bridge-nf-call-iptables = 1' 'net.ipv4.ip_forward = 1' 'vm.max_map_count = 262144' > "$PRIVATE/sysctl.conf"
sudo -n install -m 644 "$PRIVATE/sysctl.conf" /etc/sysctl.d/90-gopulse-kubernetes.conf
sudo -n sysctl -p /etc/sysctl.d/90-gopulse-kubernetes.conf
sudo -n install -d -m 700 /etc/gopulse-kubernetes /var/lib/gopulse-kubernetes
cat > "$PRIVATE/containerd.toml" <<'EOF'
version = 3
root = "/var/lib/gopulse-kubernetes/containerd"
state = "/run/gopulse-containerd"
[grpc]
  address = "/run/gopulse-containerd/containerd.sock"
[plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes.runc]
  runtime_type = "io.containerd.runc.v2"
  [plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes.runc.options]
    SystemdCgroup = true
[plugins.'io.containerd.cri.v1.runtime'.cni]
  bin_dirs = ["/opt/cni/bin"]
  conf_dir = "/etc/gopulse-kubernetes/cni/net.d"
EOF
sudo -n install -m 600 "$PRIVATE/containerd.toml" /etc/gopulse-kubernetes/containerd.toml
python3 "$ROOT/tools/devtools/kubernetes/runtime_proxy.py" "$PRIVATE/runtime.env"
sudo -n install -m 600 "$PRIVATE/runtime.env" /etc/gopulse-kubernetes/runtime.env
cat > "$PRIVATE/gopulse-containerd.service" <<'EOF'
[Unit]
Description=GoPulse Kubernetes containerd
After=network-online.target
Wants=network-online.target
[Service]
EnvironmentFile=-/etc/gopulse-kubernetes/runtime.env
ExecStart=/usr/bin/containerd --config /etc/gopulse-kubernetes/containerd.toml
Restart=always
RestartSec=5
Delegate=yes
KillMode=process
LimitNOFILE=1048576
TasksMax=infinity
[Install]
WantedBy=multi-user.target
EOF
sudo -n install -m 644 "$PRIVATE/gopulse-containerd.service" /etc/systemd/system/gopulse-containerd.service
sudo -n systemctl daemon-reload
sudo -n systemctl enable --now gopulse-containerd
for attempt in $(seq 1 60); do
    if sudo -n ctr --address /run/gopulse-containerd/containerd.sock version >/dev/null 2>&1; then break; fi
    sleep 1
done
sudo -n ctr --address /run/gopulse-containerd/containerd.sock version >/dev/null
NODE_IP=$(python3 "$ROOT/tools/devtools/kubernetes/cluster.py" node-ip)
cat > "$PRIVATE/kubeadm.yaml" <<EOF
apiVersion: kubeadm.k8s.io/v1beta4
kind: InitConfiguration
localAPIEndpoint:
  advertiseAddress: $NODE_IP
  bindPort: 6443
nodeRegistration:
  name: gopulse-wsl
  criSocket: unix:///run/gopulse-containerd/containerd.sock
  kubeletExtraArgs:
    - name: node-ip
      value: $NODE_IP
---
apiVersion: kubeadm.k8s.io/v1beta4
kind: ClusterConfiguration
clusterName: gopulse
kubernetesVersion: v$VERSION
controlPlaneEndpoint: 127.0.0.1:6443
apiServer:
  certSANs: [127.0.0.1, localhost, $NODE_IP]
networking:
  podSubnet: 10.244.0.0/16
  serviceSubnet: 10.96.0.0/12
etcd:
  local:
    dataDir: /var/lib/gopulse-kubernetes/etcd
---
apiVersion: kubelet.config.k8s.io/v1beta1
kind: KubeletConfiguration
cgroupDriver: systemd
failSwapOn: false
memorySwap:
  swapBehavior: NoSwap
systemReserved:
  cpu: 500m
  memory: 768Mi
kubeReserved:
  cpu: 500m
  memory: 768Mi
EOF
chmod 600 "$PRIVATE/kubeadm.yaml"
printf '%s\n' "$ROOT" > "$PRIVATE/owner"
python3 "$ROOT/tools/devtools/kubernetes/prefetch.py" ${K8S_DIRECT:+--direct}
sudo -n kubeadm init --config "$PRIVATE/kubeadm.yaml" > "$PRIVATE/init.log" 2>&1 || { tail -40 "$PRIVATE/init.log"; exit 1; }
finish_install
