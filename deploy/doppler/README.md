# Doppler topology

`topology.json` is the names-only contract for Doppler projects, config
inheritance, and parent-owned entries. It intentionally contains no values.

Runtime workloads always use a token scoped to their service project. Projects
prefixed with `shared-` are inheritance-only and must not have workload tokens or
direct deployment integrations.

Infrastructure integrations use single-purpose projects. Their tokens cannot
read the broad legacy `infra/prd` root or unrelated branch-config entries.

The graph is flat: a service may inherit several capability parents, but parents
do not inherit other parents. Parent name sets are disjoint, so correctness never
depends on Doppler inheritance precedence.

Run the offline contract tests with:

```sh
go test ./deploy/doppler
```

With an authenticated Doppler CLI, compare the live names and inheritance graph
without fetching values:

```sh
deploy/doppler/check-live.sh
```

`shared` and `worker` are retained temporarily as inactive rollback sources.
Legacy `infra` remains until its Flux webhook and any out-of-band CLI consumers
are inventoried. Archive these projects only after the production observation
window and a service-token/integration review in the Doppler UI.
