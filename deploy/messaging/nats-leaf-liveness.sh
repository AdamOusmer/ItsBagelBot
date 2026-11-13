#!/bin/sh
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

# An account loaded with a dropped import stays that way until restart; its clients only see "no responders".
export SSL_CERT_FILE=/etc/nats/certs/ca.crt
monitor=https://localhost:8223

wget -q -T 2 -O /dev/null "$monitor/healthz" || exit 1
for account in $(wget -q -T 2 -O- "$monitor/accountz" | grep -o 'A[A-Z2-7]\{55\}'); do
  if wget -q -T 2 -O- "$monitor/accountz?acc=$account" | grep -q '"complete": false'; then
    echo "account $account is incomplete"
    exit 1
  fi
done
