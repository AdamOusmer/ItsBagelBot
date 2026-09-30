# Plain context

Plain is a Go service scaffold, not a functioning standard-tier bot engine. Its current `main` initializes shared service infrastructure, loads an unused worker config, opens Valkey, and then returns. It does not register consumers, publish actions, serve health, or wait for shutdown.

See the [shared vocabulary](../../CONTEXT.md) and [context map](../../CONTEXT-MAP.md).

## Language and navigation

- **Plain**: the service name of this unfinished binary. Standard chat processing is currently implemented in [Sesame](../twitch/sesame/context.md).
- **Lanes / Projection**: shared config blocks embedded in `Config`; they do not establish active runtime subscriptions.
- [main.go](main.go): the complete current runtime, using `svcboot.NewCore`, `config.Load`, and `svcboot.MustValkey`.
- [internal/config/config.go](internal/config/config.go): worker limits, premium reserve, special users, live TTL, and embedded infrastructure/lane/projection configuration.
- [shared boot facade](../../pkg/svcboot/svcboot.go): lifecycle conventions to use if completing the service.

## Avoid stale assumptions

The config package comment describes an ingress → pipeline → outgress worker, but that behavior is not wired in the binary. Do not route a feature here merely because a directory named Plain exists. Before implementing this service, decide its boundary against Sesame and establish its consumer group, subjects, health, and lifecycle explicitly.

Compile/test the scaffold from the repository root with `go test ./app/plain/...`; it currently has no dedicated behavior tests. Running it requires the infrastructure its boot path opens and does not provide a useful bot worker today.
