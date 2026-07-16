# Fleet node provisioning (Ansible)

Turns ONE fresh **RHEL-family** box into a hardened, tailnet-only fleet node:
SELinux + fleet optimizations, firewalld, Tailscale mesh, k3s agent joined to
node1 — then **removes system SSH entirely**.

Any RHEL-family distro works — **Rocky, Alma, Oracle, RHEL, CentOS Stream**. Pick
whatever you run your own stack on; the playbook checks `os_family == RedHat` and
uses the family's generic repos (`dnf`, `rhel/$releasever` Tailscale repo, etc).

Secrets never touch the repo. The run is wrapped with **Doppler**; the k3s token and
the Tailscale pre-auth key come from the environment.

## Debian → Red Hat is a reinstall, not an in-place convert

You cannot convert Debian to RHEL in place. For node2 (and any old node):

1. From node1, drain + delete the old node:
   ```
   kubectl drain node2 --ignore-daemonsets --delete-emptydir-data --force
   kubectl delete node node2
   ```
2. Reinstall it with any RHEL-family image (Rocky/Alma/Oracle/RHEL). Note the
   login user (Oracle on OVH = `opc`; Rocky = `rocky`; Alma = `almalinux`).
3. Provision it (below). It joins as a **brand-new node** with a fresh identity —
   rename/remove the leftover old nodes manually afterward.

The playbook refuses to run on non-RHEL hosts (pre-task assert).

## Identity: always joins as a NEW node

- Tailscale joins **pre-authorized** with a key tagged `tag:itsbagelbot`.
- Node name: pass `NODE_NAME` for an explicit incremental name (`node4`, `node5`…),
  or leave it unset and a unique `bot-XXXXX` name is generated and **persisted** to
  `/etc/fleet/node-name`. Either way the box never clobbers an existing identity.
- No IPs are copied or committed. `node-ip` is read from the live `tailscale0`
  interface at apply time.

## Direct peer-to-peer UDP is mandatory

The playbook now fails provisioning if any fleet peer remains on DERP or a peer
relay after ten probes. This is intentional: Flannel carries all cross-node pod
traffic over Tailscale, so a relayed edge would also carry NATS and JetStream
RAFT over the relay.

Before provisioning, make UDP `41641` reachable:

1. In every cloud firewall/security group, allow inbound UDP destination port
   `41641` from `0.0.0.0/0` (and `::/0` when the node has public IPv6). The host
   firewall still restricts all other public traffic; Tailscale authenticates
   and encrypts packets arriving on this UDP socket.
2. On worker1's upstream router, reserve its LAN address and create a static
   UDP port-forward from WAN `41641` to worker1 port `41641`. Enabling
   NAT-PMP/UPnP is an alternative, but a static mapping is more predictable for
   a cluster node.
3. Allow outbound UDP to arbitrary destinations and UDP `3478` for STUN.

Validate from each node (this pulls the live peer list so it never goes stale):

```bash
# Ping every fleet peer; skip yourself
self=$(tailscale status --json | jq -r '.Self.HostName')
tailscale status --json \
  | jq -r '.Peer[] | select(.OS != "") | .HostName' \
  | while read -r peer; do
      [ "$peer" = "$self" ] && continue
      tailscale ping --until-direct=true --c=10 --timeout=2s "$peer"
    done
```

Every remote peer must finish with `via <public-ip>:<udp-port>`, never
`via DERP(...)`.

When adding a node, update `tailscale_direct_peers` in `group_vars/all.yml`
**before** running the playbook so the Ansible enforcement loop includes it.

## Secrets (Doppler)

Put these in the Doppler config you run with:

| Key | What |
|-----|------|
| `K3S_TOKEN`  | node1 join token — `sudo cat /var/lib/rancher/k3s/server/node-token` |
| `TS_AUTHKEY` | Tailscale **pre-authorized** key, `ephemeral=false`, tagged `tag:itsbagelbot` |
| `NODE_ZONE`  | (optional) `topology.kubernetes.io/zone`, e.g. `ovh-bhs1` |
| `NODE_EXTERNAL_IP` | (optional) fixed, peer-reachable address for native WireGuard Flannel |
| `FLANNEL_IFACE` | (optional) direct underlay interface; defaults to `tailscale0` for NAT-safe provisioning |

Generate the auth key at <https://login.tailscale.com/admin/settings/keys> with the
`tag:itsbagelbot` tag and pre-approval. Your tailnet ACL **must grant SSH into `tag:itsbagelbot`**
(see the SSH warning below).

## Run

```bash
cd deploy/ansible
ansible-galaxy collection install -r requirements.yml

# auto-generated new-node name, default user opc:
./provision.sh 51.x.x.x

# explicit incremental name + user:
./provision.sh 51.x.x.x opc node4

# dry run first:
NODE_NAME=node4 doppler run -- ansible-playbook site.yml \
  -e target_host=51.x.x.x -e target_user=opc --check --diff
```

## Existing cluster: native WireGuard pod network

The production data plane uses K3s `wireguard-native`. Every node advertises a
direct WAN Flannel endpoint; no pod or service traffic is nested in Tailscale.
Tailscale remains only for SSH and explicit private routes such as Kubernetes
management, the database, and admin ingress. node2's API advertise address and
the API server's kubelet address preference remain pinned to Tailscale
InternalIP; `node-external-ip` must not move Kubernetes API or DB traffic onto
the public interface. Workloads, NATS, Valkey, RPC, streams, telemetry, and
autoscaling use ClusterIP/pod addresses on native WireGuard.

Stage the idempotent configuration without restarting anything:

```bash
ansible-playbook -i inventory.cluster.ini cluster-network-wireguard.yml
```

Prove bidirectional UDP 51820 reachability on every advertised endpoint before
activation:

```bash
ansible-playbook -i inventory.cluster.ini cluster-network-preflight.yml
```

After the preflight passes, activate it in K3s' required server-first order:

```bash
ansible-playbook -i inventory.cluster.ini cluster-network-wireguard.yml \
  -e activate_wireguard_native=true
```

Each node preserves `/etc/rancher/k3s/config.yaml.pre-wireguard-native`. A
server-first rollback is fully automated:

```bash
ansible-playbook -i inventory.cluster.ini cluster-network-rollback.yml
```

`provision.sh` is just a thin `doppler run -- ansible-playbook …` wrapper.

### Compute-only nodes (taint + label)

Set `NODE_POOL` to fence a node so **only pods that tolerate it** schedule there
(default-deny). Used for the private compute box — user-facing apps + ingress
stay on the cloud nodes automatically; only tolerating workloads land here.

```bash
NODE_POOL=worker-pool ./provision.sh 51.x.x.x opc node4
```

This adds at k3s registration:
```
node-label: itsbagelbot.dev/pool=worker-pool
node-taint: itsbagelbot.dev/pool=worker-pool:NoSchedule
```
Pods opt in with a matching toleration (already added to the production compute
Deployments + `nats-leaf`). `linkerd-cni` / `host-research` tolerate all taints, so
mesh + CNI work; `falco` / `crowdsec-agent` were patched to tolerate the pool too.
Leave `NODE_POOL` unset for normal cloud nodes (no taint).

## ⚠️ SSH is removed — read before running

The final role (`ssh_removal`) **stops, masks, and uninstalls OpenSSH**, and drops
the firewall SSH rule. It runs only after gating on:

- firewalld is `running`
- Tailscale `BackendState == Running`
- node is tagged `tag:itsbagelbot`

After it completes the box is reachable **only** via:

- **Tailscale SSH** (`tailscale up --ssh` is enabled during provisioning) — this
  works **only if your tailnet ACL allows SSH into `tag:itsbagelbot`**. Verify that ACL
  BEFORE running or you lock yourself out.
- **OVH KVM / rescue console** (break-glass).

To keep system SSH (e.g. first test run), set `remove_system_ssh=false`:
```bash
./provision.sh 51.x.x.x opc node4 -e remove_system_ssh=false
```

## Layout

```
ansible.cfg
inventory.ini          # single target via -e target_host / target_user
group_vars/all.yml     # versions, IPs, env-sourced secrets, toggles
provision.sh           # doppler run wrapper
site.yml               # base → selinux → firewall → tailscale → k3s_agent → ssh_removal
roles/
  base/        identity (persisted), hostname, packages, modules, fleet sysctl, ulimits
  selinux/     enforcing + targeted + container-selinux + k3s-selinux (node1 parity)
  firewall/    firewalld: tailscale0 trusted; public = ssh (bootstrap) + tailscale udp
  tailscale/   install, pre-auth join tag:itsbagelbot, Tailscale SSH on
  k3s_agent/   pinned agent, hardened kubelet args, node-ip from live tailscale0
  ssh_removal/ verify firewall+tailscale up, then rip out OpenSSH
```

node1 (the k3s server) is the control plane, not an agent — it is **not** a target
here. This playbook only provisions agents.
```
