#!/usr/bin/env bash
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
# Render the real cluster inventory from the committed template, substituting
# node addresses from the environment (Doppler injects them).
#
#   cd deploy/ansible
#   doppler run --project infra-bootstrap --config prd -- ./render-inventory.sh
#
# The output, inventory.cluster.ini, is gitignored. This repository is public,
# so node addresses must not be committed: together they disclose which hosts
# form one cluster and which single host is the control plane, and one of the
# retired nodes was a home connection.
#
# Required in Doppler infra-bootstrap/prd:
#   NODE1_PUBLIC_IP  NODE1_TAILNET_IP  NODE4_PUBLIC_IP  NODE5_PUBLIC_IP  NODE6_PUBLIC_IP
set -euo pipefail

cd "$(dirname "$0")"
TEMPLATE=inventory.cluster.ini.example
OUT=inventory.cluster.ini

[ -f "$TEMPLATE" ] || { echo "missing $TEMPLATE" >&2; exit 1; }

required=(NODE1_PUBLIC_IP NODE1_TAILNET_IP NODE4_PUBLIC_IP NODE5_PUBLIC_IP NODE6_PUBLIC_IP)
missing=()
for v in "${required[@]}"; do
  [ -n "${!v:-}" ] || missing+=("$v")
done
if [ ${#missing[@]} -gt 0 ]; then
  echo "unset: ${missing[*]}" >&2
  echo "run under: doppler run --project infra-bootstrap --config prd -- $0" >&2
  exit 1
fi

# Strip the template-only preamble, then substitute. The range ends at [cluster]
# and excludes it: anchoring on a blank line instead would run past the host
# lines (the preamble has no trailing blank) and silently emit an inventory with
# no hosts at all.
sed '/^# TEMPLATE ONLY/,/^\[cluster\]/{/^\[cluster\]/!d;}' "$TEMPLATE" \
  | sed \
      -e "s|{{ NODE1_PUBLIC_IP }}|${NODE1_PUBLIC_IP}|g" \
      -e "s|{{ NODE1_TAILNET_IP }}|${NODE1_TAILNET_IP}|g" \
      -e "s|{{ NODE4_PUBLIC_IP }}|${NODE4_PUBLIC_IP}|g" \
      -e "s|{{ NODE5_PUBLIC_IP }}|${NODE5_PUBLIC_IP}|g" \
      -e "s|{{ NODE6_PUBLIC_IP }}|${NODE6_PUBLIC_IP}|g" \
  > "$OUT"

# Fail loudly rather than handing ansible a file with a literal placeholder in it.
if grep -q '{{' "$OUT"; then
  echo "unsubstituted placeholders remain in $OUT:" >&2
  grep -n '{{' "$OUT" >&2
  rm -f "$OUT"
  exit 1
fi

chmod 600 "$OUT"
echo "rendered $OUT ($(sed -n '/^\[cluster\]/,/^\[/p' "$OUT" | grep -c '^node') hosts)"
