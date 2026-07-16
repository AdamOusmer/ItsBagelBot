#!/usr/bin/env bash
# Preserve Tailscale's documented Linux forwarding offload on the physical
# underlay. The k3s pod path is VXLAN -> tailscale0 -> this device, so disabling
# UDP GRO forwarding forces avoidable packet-per-packet work after every boot.
set -euo pipefail

IP=/usr/sbin/ip
ETHTOOL=/usr/sbin/ethtool

dev=${TAILSCALE_UNDERLAY_DEV:-}
if [[ -z $dev ]]; then
  dev=$($IP -o route get 8.8.8.8 | awk '{print $5; exit}')
fi
[[ -n $dev && -e /sys/class/net/$dev ]] || {
  echo "unable to find Tailscale underlay device" >&2
  exit 1
}

$ETHTOOL -K "$dev" rx-udp-gro-forwarding on rx-gro-list off
$ETHTOOL -k "$dev" | awk '
  /^rx-gro-list:/ { gro_list = $2 }
  /^rx-udp-gro-forwarding:/ { udp_gro = $2 }
  END {
    if (gro_list != "off" || udp_gro != "on") exit 1
  }
'
