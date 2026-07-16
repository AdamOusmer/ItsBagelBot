#!/usr/bin/env bash
# Reboot to the saved known-good kernel when a one-time qualification boot
# cannot restore the complete worker1 network and Kubernetes path.
set -euo pipefail

marker=/var/lib/worker1-kernel-qualification/target
[[ -s $marker ]] || exit 0

target=$(<"$marker")
if [[ $(uname -r) != "$target" ]]; then
  rm -f "$marker"
  exit 0
fi

for _ in {1..90}; do
  if /usr/sbin/iw dev wlp2s0 link | grep -q '^Connected to ' \
    && systemctl is-active --quiet tailscaled.service \
    && timeout 3 /usr/bin/tailscale ping -c 1 100.95.95.9 >/dev/null 2>&1 \
    && systemctl is-active --quiet k3s-agent.service \
    && systemctl is-active --quiet worker1-wifi-performance.service \
    && systemctl is-active --quiet tailscale-udp-gro.service \
    && systemctl is-active --quiet fleet-shape.service; then
    mkdir -p /var/lib/worker1-kernel-qualification
    printf '%s\n' "$(date -u +%FT%TZ) $target passed" \
      >/var/lib/worker1-kernel-qualification/last-success
    rm -f "$marker"
    exit 0
  fi
  sleep 2
done

printf '%s\n' "$(date -u +%FT%TZ) $target failed; rebooting to saved default" \
  >/var/lib/worker1-kernel-qualification/last-failure
rm -f "$marker"
systemctl reboot
