#!/usr/bin/env bash
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
# Both eight-queue NICs use the same queue-to-CPU mapping. Keep receive,
# transmit completion and CPU-based transmit selection together without RPS IPIs.
set -euo pipefail

die() { echo "fleet-irq-affinity: $*" >&2; exit 1; }

virtio_of() {
  local resolved
  resolved=$(readlink -f "$1") || die "cannot resolve $1"
  [[ $resolved =~ (virtio[0-9]+) ]] || die "no virtio device in $resolved"
  echo "${BASH_REMATCH[1]}"
}

irqs_matching() {
  local mode=$1 key=$2 irqs
  irqs=$(awk -v mode="$mode" -v key="$key" '
    (mode == "exact" && $NF == key) || (mode == "prefix" && index($NF, key) == 1) { sub(":", "", $1); print $1 }
  ' /proc/interrupts)
  [[ -n $irqs ]] || die "no IRQ matching $mode $key in /proc/interrupts"
  echo "$irqs"
}

pin() {
  local core=$1 mode=$2 key=$3 irqs irq
  irqs=$(irqs_matching "$mode" "$key")
  while read -r irq; do
    if printf '%x\n' $((1 << core)) > "/proc/irq/$irq/smp_affinity"; then
      [[ $(cat "/proc/irq/$irq/smp_affinity_list") == "$core" ]] || die "IRQ $irq ($key) did not take core $core"
    else
      # Kernel-managed IRQs (virtio-scsi request queue) refuse writes; accept only if already effective on $core.
      [[ $(cat "/proc/irq/$irq/effective_affinity_list") == "$core" ]] || die "managed IRQ $irq ($key) effective on $(cat "/proc/irq/$irq/effective_affinity_list"), want $core"
      echo "IRQ $irq ($key) is kernel-managed, already effective on core $core"
    fi
  done <<< "$irqs"
}

set_mask() {
  printf '%x\n' "$2" > "$1"
}

ETH0=$(virtio_of /sys/class/net/eth0/device)
ETH1=$(virtio_of /sys/class/net/eth1/device)
STORAGE=$(virtio_of /sys/block/sda/device)
[[ $ETH0 != "$ETH1" && $ETH0 != "$STORAGE" && $ETH1 != "$STORAGE" ]] || die "virtio mapping collision: $ETH0 $ETH1 $STORAGE"

# Validate the fleet's 12-vCPU/eight-queue layout before changing any affinity.
[[ $(getconf _NPROCESSORS_ONLN) -ge 12 ]] || die "this CPU plan requires 12 online CPUs"
for dev in eth0 eth1; do
  rx=(/sys/class/net/"$dev"/queues/rx-*)
  tx=(/sys/class/net/"$dev"/queues/tx-*)
  [[ ${#rx[@]} -eq 8 && ${#tx[@]} -eq 8 ]] || die "$dev requires eight RX and TX queues"
done

balloon_dirs=(/sys/bus/virtio/drivers/virtio_balloon/virtio*)
[[ -e ${balloon_dirs[0]} ]] || die "virtio_balloon device not found"
BALLOON=$(basename "${balloon_dirs[0]}")

pin 0 prefix "${BALLOON}-"
pin 11 prefix "${STORAGE}-"

for dev in eth0 eth1; do
  virtio=$(virtio_of "/sys/class/net/$dev/device")
  for q in 0 1 2 3 4 5 6 7; do
    core=$((q + 2))
    pin "$core" exact "${virtio}-input.${q}"
    pin "$core" exact "${virtio}-output.${q}"
    set_mask "/sys/class/net/$dev/queues/tx-$q/xps_cpus" $((1 << core))
    set_mask "/sys/class/net/$dev/queues/rx-$q/rps_cpus" 0
    echo 0 > "/sys/class/net/$dev/queues/rx-$q/rps_flow_cnt"
  done

  # CPUs 0, 10 and 11 share these queues with their completion CPU on both NICs.
  # CPU 1 has no explicit XPS entry and uses the kernel's fallback selection.
  # Guest topology exposes one L3 domain; this does not pin host physical CPUs.
  set_mask "/sys/class/net/$dev/queues/tx-0/xps_cpus" 0x5
  set_mask "/sys/class/net/$dev/queues/tx-6/xps_cpus" 0x500
  set_mask "/sys/class/net/$dev/queues/tx-7/xps_cpus" 0xa00
done
