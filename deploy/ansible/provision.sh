#!/usr/bin/env bash
# Provision a fresh RHEL-family box into the fleet, secrets injected by Doppler.
#
# Usage:
#   ./provision.sh <target_host> [target_user] [node_name]
#
# Examples:
#   ./provision.sh 51.x.x.x                 # auto-generated new node name, user=opc
#   ./provision.sh 51.x.x.x opc node4       # explicit incremental name
#
# Doppler project infra-bootstrap/prd must hold: K3S_TOKEN, TS_AUTHKEY
# (TS_AUTHKEY = preauth key, tag:itsbagelbot).
# Optional in Doppler/env:  NODE_ZONE
#   NODE_POOL=worker-pool  -> taint+label the node so only tolerating pods land on it
set -euo pipefail

HOST="${1:?usage: ./provision.sh <target_host> [target_user] [node_name]}"
USER_="${2:-opc}"
# Fall back to an already-exported NODE_NAME rather than clobbering it with an
# empty string: a bare `NODE_NAME="${3:-}"` silently discarded the caller's
# NODE_NAME=node4 and the box persisted a generated bot-XXXXX name instead
# (2026-07-25). The generated name is only correct when nobody asked for one.
NODE_NAME="${3:-${NODE_NAME:-}}"

cd "$(dirname "$0")"

command -v doppler >/dev/null || { echo "doppler CLI not found"; exit 1; }

EXTRA=(-e "target_host=${HOST}" -e "target_user=${USER_}")

# These flow through the environment (lookup('env',...) in group_vars). doppler
# run inherits the parent env, so exporting here is enough.
#   NODE_ZONE / NODE_REGION   -> topology labels; the whole fleet is mtl1/ca-mtl
#   NODE_EXTERNAL_IP          -> peer-reachable WireGuard endpoint (required)
#   FLANNEL_IFACE             -> physical underlay iface, never tailscale0
export NODE_NAME NODE_POOL="${NODE_POOL:-}"
export NODE_ZONE="${NODE_ZONE:-}" NODE_REGION="${NODE_REGION:-}"
export NODE_EXTERNAL_IP="${NODE_EXTERNAL_IP:-}" FLANNEL_IFACE="${FLANNEL_IFACE:-}"

exec doppler run --project infra-bootstrap --config prd -- \
  ansible-playbook site.yml "${EXTRA[@]}" "${@:4}"
