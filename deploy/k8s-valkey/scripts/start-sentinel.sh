#!/bin/sh
set -e

HEADLESS="valkey-headless.valkey.svc.cluster.local"
MY_FQDN="${HOSTNAME}.${HEADLESS}"
CORE="$(cat /secrets/core/valkey-password)"
ORDINAL="${HOSTNAME##*-}"
MY_ID="$(printf '%s' "$HOSTNAME" | sha1sum | awk '{print $1}')"

# Map ordinal to the node's Tailscale IP so the external witness can reach this
# sentinel over the encrypted Tailscale tunnel.
if [ "$ORDINAL" = "0" ]; then
  ANNOUNCE_IP="100.98.67.104"    # node1 tailscale
elif [ "$ORDINAL" = "1" ]; then
  ANNOUNCE_IP="100.83.72.45"     # node2 tailscale
elif [ "$ORDINAL" = "2" ]; then
  ANNOUNCE_IP="100.81.255.104"   # node3 tailscale
else
  ANNOUNCE_IP="$MY_FQDN"
fi

cp /config/sentinel.conf /tmp/sentinel.conf
chmod 600 /tmp/sentinel.conf

cat >> /tmp/sentinel.conf << EOF
sentinel auth-pass myprimary ${CORE}
requirepass ${CORE}
sentinel announce-ip ${ANNOUNCE_IP}
sentinel announce-port 26379
sentinel myid ${MY_ID}
EOF

# Note: the monitor address stays the stable headless FQDN
# (valkey-node-0.valkey-headless...) from /config/sentinel.conf with
# resolve-hostnames yes. Do NOT rewrite it to a pod IP: pod IPs change on
# restart and Sentinel would pin to a dead address, deadlocking master
# discovery. Seeding with node-0's FQDN is fine even when node-0 is currently a
# replica; Sentinel follows the real master from there and tracks failovers.

exec valkey-server /tmp/sentinel.conf --sentinel
