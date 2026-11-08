# OS worker isolation

`sandbox` owns process launch and kernel isolation. It does not own credentials,
application protocols, network destinations or restart policy. Callers use
`Start` with an absolute worker executable and explicitly inherited pipe ends.
`Process.Input` and `Output` are private control pipes. Cancellation and `Close`
kill the worker; `Wait` joins its exit. There is no unrestricted fallback.

The worker calls `ChildIsolated` before reading configuration or credentials.
`Pipe`, `OpenPipe` and `ControlPipes` keep adopted handles pollable so bounded
protocol reads/writes and cancellation work across process boundaries. Only
explicit pipe descriptors are inherited, with at most eight extra pipe ends.

| Platform | Required boundary | Validation here |
| --- | --- | --- |
| macOS | Separately signed nested App Sandbox app, hardened runtime, exact deny-only policy, runtime probes | Real arm64 process, Keychain/CLI, file, parent-memory, exec and network denial; local TLS and existing Tunnel integration |
| Linux amd64/arm64 | Static helper, bubblewrap user/PID/network/mount namespaces, minimal read-only filesystem, no capabilities, seccomp and no-new-privileges | Cross-compilation; native kernel tests still required |
| Windows amd64/arm64 | Zero-capability AppContainer, explicit inherited handles, restricted child creation, process-owned job | Cross-compilation; native kernel tests still required |

The worker cannot open network sockets. Its application can grant fixed raw
stream capabilities through pipes; destination selection remains in the trusted
parent. The desktop broker and typed IPC are documented in
[internal/network](../../app/desktop/internal/network/README.md).

Windows also applies a one-process job, a hard 128 MiB process-memory limit and
a 20% CPU rate cap. Linux/macOS set Go runtime memory/concurrency policies;
these are not hard kernel memory or CPU limits. Platform isolation does not
remove the need for bounded IPC, restart budgets or safe parent-side decoding.

See [Darwin policy, evidence and release requirements](DARWIN.md). Its temporary
SBPL entitlement is implementation-dependent; real Developer ID signing,
notarization and supported OS versions must be validated before distribution.
Unsupported platforms, missing prerequisites or failed isolation probes return
an error. Cross-compilation does not certify enforcement by another kernel.

```sh
go test -race ./pkg/sandbox
go vet ./pkg/sandbox
# On native Linux/Windows, enable the opt-in kernel fixture:
BAGEL_SANDBOX_KERNEL_TEST=1 go test ./pkg/sandbox -count=1 -v
```
