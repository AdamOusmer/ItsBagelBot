# Valkey fleet p99 acceptance

This probe measures the real shared Go client from every production node. It
separates the two routes that have different physical limits:

- `read-local` uses the TLS `valkey-local` Service and its same-node endpoint;
- `write-master` uses Sentinel and waits for the elected primary.

The isolated key has a five-minute TTL and is deleted after each run. Temporary
pods are pinned to the requested node, use the production CA and ACL password,
match Sesame's two-CPU limit, and are cleaned up automatically.

Run a diagnostic fleet pass without failing on a missed target:

```sh
deploy/k8s/valkey-live-acceptance/run.sh
```

Make sub-2 ms p99 a hard acceptance gate:

```sh
REQUIRE_TARGET=true TARGET=2ms deploy/k8s/valkey-live-acceptance/run.sh
```

`NODES`, `CONCURRENCY`, `REQUESTS`, `WARMUP`, and `MODE=read|write|both` are
configurable. A write failure on a remote node is an architectural signal, not
a reason to enable replica writes: those writes are local-only and can be lost
or overwritten during resynchronization.
