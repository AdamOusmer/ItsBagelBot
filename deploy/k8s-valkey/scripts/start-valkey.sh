#!/bin/sh
set -e

HEADLESS="valkey-headless.valkey.svc.cluster.local"
SENTINEL_SVC="valkey.valkey.svc.cluster.local"
MY_FQDN="${HOSTNAME}.${HEADLESS}"
ORDINAL="${HOSTNAME##*-}"
CORE="$(cat /secrets/core/valkey-password)"

# Map ordinal to the node's Tailscale IP so the external (Tailscale-only)
# witness sentinel and cross-node replicas can reach this instance directly.
if [ "$ORDINAL" = "0" ]; then
  ANNOUNCE_IP="100.98.67.104"    # node1 tailscale
elif [ "$ORDINAL" = "1" ]; then
  ANNOUNCE_IP="100.83.72.45"     # node2 tailscale
elif [ "$ORDINAL" = "2" ]; then
  ANNOUNCE_IP="100.81.255.104"   # node3 tailscale
else
  ANNOUNCE_IP="$MY_FQDN"
fi

CURRENT_PRIMARY=$(
  REDISCLI_AUTH="$CORE" valkey-cli -h "$SENTINEL_SVC" -p 26379 \
    SENTINEL get-primary-addr-by-name myprimary 2>/dev/null | head -1 || true
)

if [ -z "$CURRENT_PRIMARY" ]; then
  # No master known yet (cold cluster): ordinal 0 is the default master.
  if [ "$ORDINAL" = "0" ]; then
    ROLE=primary
  else
    ROLE=replica
    PRIMARY_HOST="valkey-node-0.${HEADLESS}"
  fi
elif [ "$CURRENT_PRIMARY" = "$ANNOUNCE_IP" ]; then
  # Sentinel tracks the master by its Tailscale announce IP, so compare against
  # ours. (Pod IPs change on restart and must not be used here.)
  ROLE=primary
else
  ROLE=replica
  PRIMARY_HOST="$CURRENT_PRIMARY"
fi

cp /config/valkey.conf /data/valkey.conf
chmod 600 /data/valkey.conf

cat >> /data/valkey.conf << EOF
masterauth ${CORE}
replica-announce-ip ${ANNOUNCE_IP}
replica-announce-port 6379
EOF

if [ "$ROLE" = "replica" ]; then
  echo "replicaof ${PRIMARY_HOST} 6379" >> /data/valkey.conf
fi

exec valkey-server /data/valkey.conf
