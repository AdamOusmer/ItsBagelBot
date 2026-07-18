# Valkey fleet p99 acceptance

This probe measures the real shared Go client from every production node. It
separates the two routes that have different physical limits:

- `read-local` uses the TLS `valkey-local` Service and its same-node endpoint;
- `write-master` uses Sentinel and waits for the elected primary.

The isolated key has a five-minute TTL and is deleted after each run. Temporary
pods are pinned to the requested node, use the production CA and ACL password,
match Sesame's two-CPU limit, and are cleaned up automatically. The runner
requires one immutable digest of the CI-published multi-architecture image, so
it never compiles on the operator's computer, copies an executable into a pod,
or silently changes the executable between qualification runs.

Run a diagnostic fleet pass without failing on a missed target:

```sh
CONFIRM_LIVE_VALKEY_BENCH=I-understand-this-runs-on-live-Valkey \
BENCH_IMAGE=ghcr.io/adamousmer/itsbagelbot/valkey-live-acceptance@sha256:<digest> \
deploy/k8s/valkey-live-acceptance/run.sh
```

Make sub-2 ms p99 a hard acceptance gate:

```sh
CONFIRM_LIVE_VALKEY_BENCH=I-understand-this-runs-on-live-Valkey \
BENCH_IMAGE=ghcr.io/adamousmer/itsbagelbot/valkey-live-acceptance@sha256:<digest> \
REQUIRE_TARGET=true TARGET=2ms deploy/k8s/valkey-live-acceptance/run.sh
```

`NODES`, `CONCURRENCY`, `REQUESTS`, `WARMUP`, `CPU_REQUEST`,
`POD_TIMEOUT_SECONDS`, `CLEANUP_TIMEOUT_SECONDS`, `MODE=read|write|both`, and
`WRITE_PROFILE=pooled|pipeline` are configurable. The default `pooled` profile
matches reply-critical production mutations; `pipeline` selectively exercises
valkey-go's automatic pipeline without changing the deployed client default.
`BENCH_IMAGE` must use `repository@sha256:digest`; mutable tags are rejected
before any pod is created. A write failure on a remote node is an architectural
signal, not a reason to enable replica writes: those writes are local-only and
can be lost or overwritten during resynchronization.
