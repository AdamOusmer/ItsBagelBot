# WARP sidecar context

This directory builds the Cloudflare WARP client image used beside Gossip. The image supplies vendor executables; [Gossip's deployment](../../deploy/k8s/gossip.yaml) owns daemon startup, registration, proxy mode, health probes, resources, and image pinning. There is no Go application here.

See the [shared vocabulary](../../CONTEXT.md) and [context map](../../CONTEXT-MAP.md).

## Language

- **WARP sidecar**: the container supplying Gossip's untrusted HTTP egress proxy.
- **Proxy mode**: a userspace SOCKS route bound to pod loopback (`127.0.0.1:40000`), without a TUN device or `NET_ADMIN`.
- **Image digest**: the deployment's immutable selection of built bytes. Rebuilding the image does not update running pods.
- **Anonymous RSS**: resident process memory, distinct from virtual mappings and reclaimable cgroup file cache.

## Code and ownership

| Location | Responsibility |
| --- | --- |
| [Containerfile](Containerfile) | Wraps the Debian vendor package and verifies `warp-cli` is installed; deliberately defines no entrypoint/command. |
| [README.md](README.md) | Dated memory investigation, allocator changes, and operational observation commands. |
| [Gossip deployment](../../deploy/k8s/gossip.yaml) | Runtime bootstrap and security/resource settings. |
| [Gossip HTTP transport](../gossip/internal/core/http.go) | Guarded DNS/destination handling, SOCKS routing, and fail-closed behavior. |
| [Gossip guide](../gossip/context.md) | Provider trust declarations and application-level request flow. |

## Editing rules and validation

- Keep the listener on pod loopback; Gossip routes pinned, locally validated destination IPs through it. The proxy itself is not a substitute for the application's destination checks.
- Preserve proxy mode and the deployment's restricted capabilities. A dead WARP route must not silently switch untrusted fetches to direct cluster egress.
- Do not remove package dependencies such as `gnupg` through cleanup/autoremove; the Containerfile explains how doing so can uninstall WARP itself.
- The package channel is resolved at build time; the deployed image digest controls rollout. Check the manifest rather than guessing the live vendor version from this source.
- Listener reachability proves a bound socket, not a healthy external tunnel. Memory conclusions require same-process observations over time; a restart resetting RSS is not proof of a leak fix.

From the repository root, build with `podman build -t itsbagelbot/warp -f app/warp/Containerfile .`. Relevant source checks are `go test ./app/gossip/internal/core ./deploy/k8s`; image execution and tunnel behavior require a container/runtime environment.
