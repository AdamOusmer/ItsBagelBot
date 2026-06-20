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
# Doppler must hold:  K3S_TOKEN, TS_AUTHKEY   (TS_AUTHKEY = preauth key, tag:itsbagelbot)
# Optional in Doppler/env:  NODE_ZONE
set -euo pipefail

HOST="${1:?usage: ./provision.sh <target_host> [target_user] [node_name]}"
USER_="${2:-opc}"
NODE_NAME="${3:-}"

cd "$(dirname "$0")"

command -v doppler >/dev/null || { echo "doppler CLI not found"; exit 1; }

EXTRA=(-e "target_host=${HOST}" -e "target_user=${USER_}")

# NODE_NAME flows through the environment (read via lookup('env',...) in group_vars)
export NODE_NAME

exec doppler run -- ansible-playbook site.yml "${EXTRA[@]}" "${@:4}"
