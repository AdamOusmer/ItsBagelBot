#!/usr/bin/env bash
# Cap worker1 at 500 Mbit/s in each direction while reserving 20 Mbit/s for
# control traffic. IFB registration is asynchronous on rtw88, so wait for the
# device before installing ingress filters.
set -euo pipefail

TC=/usr/sbin/tc
IP=/usr/sbin/ip
MODPROBE=/usr/sbin/modprobe
UP=500mbit
DOWN=500mbit

"$MODPROBE" ifb numifbs=0

wait_for_link() {
  local link=$1
  for _ in {1..50}; do
    [[ -e /sys/class/net/$link ]] && return 0
    sleep 0.05
  done
  echo "timed out waiting for $link" >&2
  return 1
}

shape() {
  local dev=$1 ifb
  ifb=$(printf 'ifb-%s' "$dev" | cut -c1-15)

  "$TC" qdisc del dev "$dev" root 2>/dev/null || true
  "$TC" qdisc del dev "$dev" ingress 2>/dev/null || true
  "$IP" link del "$ifb" 2>/dev/null || true

  "$TC" qdisc add dev "$dev" root handle 1: htb default 20 r2q 1000
  "$TC" class add dev "$dev" parent 1: classid 1:1 htb rate "$UP" ceil "$UP" burst 32k cburst 32k
  "$TC" class add dev "$dev" parent 1:1 classid 1:10 htb rate 20mbit ceil "$UP" prio 0 burst 32k cburst 32k
  "$TC" class add dev "$dev" parent 1:1 classid 1:20 htb rate 480mbit ceil "$UP" prio 1 burst 32k cburst 32k
  "$TC" qdisc add dev "$dev" parent 1:10 handle 110: fq_codel
  "$TC" qdisc add dev "$dev" parent 1:20 handle 120: fq_codel
  "$TC" filter add dev "$dev" parent 1: protocol ip prio 1 u32 match ip protocol 1 0xff flowid 1:10
  "$TC" filter add dev "$dev" parent 1: protocol ip prio 2 u32 match u16 0x0000 0xffc0 at 2 flowid 1:10

  "$IP" link add "$ifb" type ifb
  wait_for_link "$ifb"
  "$IP" link set "$ifb" up
  "$TC" qdisc add dev "$dev" handle ffff: ingress
  "$TC" filter add dev "$dev" parent ffff: protocol all u32 match u32 0 0 action mirred egress redirect dev "$ifb"
  "$TC" qdisc add dev "$ifb" root handle 1: htb default 20 r2q 1000
  "$TC" class add dev "$ifb" parent 1: classid 1:1 htb rate "$DOWN" ceil "$DOWN" burst 32k cburst 32k
  "$TC" class add dev "$ifb" parent 1:1 classid 1:10 htb rate 20mbit ceil "$DOWN" prio 0 burst 32k cburst 32k
  "$TC" class add dev "$ifb" parent 1:1 classid 1:20 htb rate 480mbit ceil "$DOWN" prio 1 burst 32k cburst 32k
  "$TC" qdisc add dev "$ifb" parent 1:10 handle 110: fq_codel
  "$TC" qdisc add dev "$ifb" parent 1:20 handle 120: fq_codel
  "$TC" filter add dev "$ifb" parent 1: protocol ip prio 1 u32 match ip protocol 1 0xff flowid 1:10
  "$TC" filter add dev "$ifb" parent 1: protocol ip prio 2 u32 match u16 0x0000 0xffc0 at 2 flowid 1:10
}

for dev_path in /sys/class/net/*; do
  dev=${dev_path##*/}
  case "$dev" in
    lo|tailscale0|ifb*|cni*|flannel*|veth*|kube*|docker*|cali*) continue ;;
  esac
  [[ -e $dev_path/device ]] || continue
  shape "$dev"
  echo "shaped $dev"
done
