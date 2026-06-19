#!/bin/sh
CORE="$(cat /secrets/core/valkey-password)"
REDISCLI_AUTH="$CORE" valkey-cli -h 127.0.0.1 -p 6379 ping | grep -q PONG
