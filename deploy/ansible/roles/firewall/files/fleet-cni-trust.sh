#!/bin/sh
# Pin the flannel/CNI interfaces to firewalld's trusted zone.
#
# firewalld starts at boot BEFORE k3s creates cni0 and the flannel transport, so
# those interfaces are born into the default (public) zone every boot. firewalld
# does not retro-apply a permanent zone binding to an interface that appears
# later, so without this they stay in public and firewalld drops the cross-node
# pod traffic that rides them -- including the NATS leaf-to-leaf cluster on 6222
# that carries the outgress rate-limit permit borrow. A dropped bind silently
# fails the fleet's quota sharing open.
#
# The pod/service CIDR *sources* are trusted permanently in the firewall role,
# which covers INPUT; this closes the interface/FORWARD gap and survives reboots
# because the unit runs on every boot. Bind permanently (so a firewalld --reload
# keeps it) and at runtime (so it takes effect now).
set -eu

# Candidate interfaces. cni0 (bridge) is always present; the flannel transport is
# flannel.1 on vxlan nodes OR flannel-wg on wireguard-backend nodes -- bind
# whichever exist, never require a specific one.
CANDIDATES="cni0 flannel.1 flannel-wg"

# bind_iface trusts one interface idempotently: --add-interface errors with a
# non-zero ALREADY_ENABLED on re-runs (which set -e would treat as fatal), so add
# to the permanent config only when absent; --change-interface is a no-op-safe
# move for the runtime binding.
bind_iface() {
    _iface="$1"
    if ! firewall-cmd --permanent --zone=trusted --query-interface="${_iface}" >/dev/null 2>&1; then
        firewall-cmd --permanent --zone=trusted --add-interface="${_iface}" >/dev/null
    fi
    firewall-cmd --zone=trusted --change-interface="${_iface}" >/dev/null
    echo "fleet-cni-trust: bound ${_iface} to trusted zone"
}

iface_exists() { [ -e "/sys/class/net/$1" ]; }

# Wait (bounded) for the bridge and a flannel transport to come up after k3s
# starts; never hang a boot indefinitely (the unit retries on failure).
tries=0
while :; do
    if iface_exists cni0 && { iface_exists flannel.1 || iface_exists flannel-wg; }; then
        break
    fi
    tries=$((tries + 1))
    if [ "${tries}" -ge 90 ]; then
        break
    fi
    sleep 1
done

bound_any=0
for iface in ${CANDIDATES}; do
    if iface_exists "${iface}"; then
        bind_iface "${iface}"
        bound_any=1
    fi
done

if [ "${bound_any}" -eq 0 ]; then
    echo "fleet-cni-trust: no CNI/flannel interface present yet; will retry" >&2
    exit 1
fi
exit 0
