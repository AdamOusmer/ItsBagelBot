<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# Replace `cilium-vlan-peer-routes` with native NetworkManager routes

**This is a one-time, hand-run migration procedure, not automation.** It lives
here because `deploy/ansible/` is this repo's existing home for host/node
material and every other file this touches (the private VLAN interface, the
node inventory) is already documented there -- not because it should stay a
markdown runbook long-term. Once it has been run against the fleet, delete
this file; the standing rule on this repo is that runbooks rot and only code
and tests stay on `main`. It was NOT folded into an Ansible role (unlike
`roles/cilium_node_routes`, which does the same class of thing) because doing
so correctly needs the current real inventory, and this branch only has
`inventory.cluster.ini.example`, whose `[cluster]` section still names the
VLAN trio `node4`/`node5`/`node6` with `node1` as the OCI control plane --
that does not match the `node1`/`node2`/`node3` naming this migration was
scoped against. Reconcile that naming before or while turning this into a
role; do not assume the two schemes refer to the same hosts without checking.

## Why NetworkManager instead of the systemd unit

`/usr/local/sbin/cilium-vlan-peer-routes` + `cilium-vlan-peer-routes.service`
install static host routes at boot, once, as a `oneshot` unit, and nothing
re-asserts them afterward. That is a real failure class, not a theoretical
one: the routes vanishing once already silently isolated a node -- strict
`rp_filter` ate the asymmetric replies, and the only tell was
`wg show <iface> dump` reporting `rx>0, tx>0, handshake=0` (data flowing one
way, no fresh handshake). NetworkManager owns interface `eth1` already (see
`roles/private_vlan`), and it reapplies everything in a connection profile,
including static routes, on every activation -- boot, `connection up`,
`device reapply`, NetworkManager restart -- so the class of "routes present
at some point, silently gone later" goes away.

**The live routing table does not change.** The `ip route replace` step
below installs the exact same route the old script installs. Only where it
is *persisted* changes: from a oneshot unit to the NetworkManager profile.
That is deliberate -- it keeps this migration a no-op for traffic while it
is in flight.

## Address map

Placeholders match the `{{ VAR }}` convention already used in
`inventory.cluster.ini.example` and `render-inventory.sh`
(`NODE1_PUBLIC_IP`, etc.) -- this repo is public, and per that file's own
warning, committed node addresses "disclose which hosts form one cluster and
which single host is the control plane." Resolve every placeholder from
Doppler (`infra-bootstrap/prd`) or your own notes before running anything
below; do not hardcode the real values into a copy of this file that gets
committed.

| Node  | Public IP placeholder | VLAN IP        |
|-------|------------------------|-----------------|
| node1 | `{{ NODE1_PUBLIC_IP }}` | `10.10.0.4`    |
| node2 | `{{ NODE2_PUBLIC_IP }}` | `10.10.0.5`    |
| node3 | `{{ NODE3_PUBLIC_IP }}` | `10.10.0.6`    |

## 0. Per node: find the real NetworkManager connection name for eth1

`roles/private_vlan` creates the profile as `conn_name: vlan-private`, but
confirm it live before running anything that assumes it -- a manually
recovered node or a re-run under a different name would make every command
below silently target the wrong profile:

```bash
nmcli -t -f NAME,DEVICE connection show --active | grep ':eth1$'
```

Use the `NAME` field from that output (`$CONN` below) in every step. Assume
`vlan-private` only after this confirms it.

## 1. Persist the routes on the connection profile (per node)

Run on each node, substituting that node's peers and its own public IP as
`src`. This only writes the stored profile -- it does not touch the running
system yet.

**node1** (peers: node2, node3):
```bash
nmcli connection modify "$CONN" +ipv4.routes "{{ NODE2_PUBLIC_IP }}/32 10.10.0.5 src={{ NODE1_PUBLIC_IP }}"
nmcli connection modify "$CONN" +ipv4.routes "{{ NODE3_PUBLIC_IP }}/32 10.10.0.6 src={{ NODE1_PUBLIC_IP }}"
```

**node2** (peers: node1, node3):
```bash
nmcli connection modify "$CONN" +ipv4.routes "{{ NODE1_PUBLIC_IP }}/32 10.10.0.4 src={{ NODE2_PUBLIC_IP }}"
nmcli connection modify "$CONN" +ipv4.routes "{{ NODE3_PUBLIC_IP }}/32 10.10.0.6 src={{ NODE2_PUBLIC_IP }}"
```

**node3** (peers: node1, node2):
```bash
nmcli connection modify "$CONN" +ipv4.routes "{{ NODE1_PUBLIC_IP }}/32 10.10.0.4 src={{ NODE3_PUBLIC_IP }}"
nmcli connection modify "$CONN" +ipv4.routes "{{ NODE2_PUBLIC_IP }}/32 10.10.0.5 src={{ NODE3_PUBLIC_IP }}"
```

The `src=` attribute is load-bearing, not cosmetic: without it the kernel
picks whatever source the route selection algorithm would normally choose for
that destination, the peer's WireGuard sees a reply from an address it does
not recognise as the tunnel's endpoint, and its own reverse-path filter drops
it -- the exact isolation failure this migration exists to make impossible.

**Rollback (per route, per node):** `-ipv4.routes` with the identical string
removes exactly that entry; nmcli matches by content, not by index. Confirm
with step 3's `nmcli -g ipv4.routes` command that a route which no longer
belongs is actually gone.
```bash
nmcli connection modify "$CONN" -ipv4.routes "{{ NODE2_PUBLIC_IP }}/32 10.10.0.5 src={{ NODE1_PUBLIC_IP }}"
```

## 2. Apply the same routes live (per node)

Installs the routes on the running system immediately, without reactivating
`$CONN` -- reactivating would flap the interface, and this node's peer
connectivity depends on it staying up during the cutover. This step alone is
what the old script already did every boot; nothing here changes behavior.

**node1:**
```bash
ip route replace {{ NODE2_PUBLIC_IP }}/32 via 10.10.0.5 dev eth1 src {{ NODE1_PUBLIC_IP }}
ip route replace {{ NODE3_PUBLIC_IP }}/32 via 10.10.0.6 dev eth1 src {{ NODE1_PUBLIC_IP }}
```

**node2:**
```bash
ip route replace {{ NODE1_PUBLIC_IP }}/32 via 10.10.0.4 dev eth1 src {{ NODE2_PUBLIC_IP }}
ip route replace {{ NODE3_PUBLIC_IP }}/32 via 10.10.0.6 dev eth1 src {{ NODE2_PUBLIC_IP }}
```

**node3:**
```bash
ip route replace {{ NODE1_PUBLIC_IP }}/32 via 10.10.0.4 dev eth1 src {{ NODE3_PUBLIC_IP }}
ip route replace {{ NODE2_PUBLIC_IP }}/32 via 10.10.0.5 dev eth1 src {{ NODE3_PUBLIC_IP }}
```

**Rollback:** re-run the identical `ip route replace` from the still-running
old unit (`systemctl start cilium-vlan-peer-routes.service`), or delete the
live route and let the old unit's next boot reinstall it:
```bash
ip route del {{ NODE2_PUBLIC_IP }}/32 dev eth1
```

## 3. Verify, on every node, before touching the old unit

```bash
nmcli -g ipv4.routes connection show "$CONN"
ip route show | grep 10.10
```

Confirm every peer's `/32` is listed in both, with the right `via` and `src`.
Then confirm the tunnel itself is healthy end to end -- this is the check
that would have caught the prior silent isolation immediately instead of
after the fact:

```bash
wg show <iface> dump   # e.g. flannel-wg; use whatever `wg show` lists here
```

Every peer line needs a recent handshake timestamp, not just nonzero
rx/tx -- `rx>0, tx>0, handshake=0` is exactly the failure this migration
exists to prevent from recurring silently.

**Do not proceed to step 4 on ANY node until step 3 passes on ALL THREE.**
The old unit is still live and still correct at this point; there is no time
pressure to remove it early, and removing it before every peer confirms is
how a partial cutover turns into the same asymmetric-route isolation this
replaces.

## 4. Disable and remove the old unit and script (only after step 3 passes fleet-wide)

Back up before deleting, so rollback does not depend on redownloading or
recreating the script from memory:

```bash
FRAGMENT=$(systemctl show -p FragmentPath --value cilium-vlan-peer-routes.service)
mkdir -p /root/cilium-vlan-peer-routes.bak
cp "$FRAGMENT" /usr/local/sbin/cilium-vlan-peer-routes /root/cilium-vlan-peer-routes.bak/
```

Then, per node:

```bash
systemctl disable --now cilium-vlan-peer-routes.service
rm -f "$FRAGMENT" /usr/local/sbin/cilium-vlan-peer-routes
systemctl daemon-reload
systemctl reset-failed cilium-vlan-peer-routes.service 2>/dev/null || true
```

**Rollback:**
```bash
cp /root/cilium-vlan-peer-routes.bak/cilium-vlan-peer-routes /usr/local/sbin/
cp /root/cilium-vlan-peer-routes.bak/cilium-vlan-peer-routes.service "$FRAGMENT"
systemctl daemon-reload
systemctl enable --now cilium-vlan-peer-routes.service
```

Re-run step 3's verification after rollback too -- restoring the unit does
not by itself prove the routes it re-asserts still match what step 1 left in
the NetworkManager profile.
