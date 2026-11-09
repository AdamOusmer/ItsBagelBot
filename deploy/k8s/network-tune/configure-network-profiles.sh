#!/usr/bin/env bash
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
# Run once on node1-3. NetworkManager stores these settings in the existing
# Ethernet profiles and applies them when it activates each connection.
# This does not reactivate connections, bounce links, or install a hook.
set -euo pipefail

for dev in eth0 eth1; do
  uuid=$(nmcli -g GENERAL.CON-UUID device show "$dev")
  [[ -n $uuid && $uuid != -- ]] || { echo "$dev has no active connection profile" >&2; exit 1; }
  nmcli connection modify "$uuid" \
    ethtool.channels-combined 8 \
    ethtool.ring-rx 1024 \
    ethtool.ring-tx 256 \
    ethtool.feature-rx on \
    ethtool.feature-tx on \
    ethtool.feature-sg on \
    ethtool.feature-tso on \
    ethtool.feature-gso on \
    ethtool.feature-gro on \
    ethtool.feature-rx-udp-gro-forwarding on \
    ethtool.feature-rx-gro-list off \
    link.tx-queue-length 5000
  echo "$dev: saved eight queues, rings, offloads and TX queue length in $uuid"
done
