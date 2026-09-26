# WARP sidecar memory

The gossip deployment owns WARP's bootstrap, allocator settings, and resource
budget in `deploy/k8s/gossip.yaml`. The image digest freezes the vendor daemon;
rebuilding the image alone does not update running pods.

## Investigation: 2026-09-26

Three 44-hour-old sidecars ran WARP 2026.7.1377.0 with no allocator environment
variables, a 64Mi memory request, and a 256Mi limit. The allocator settings in
the local manifest had not been deployed.

| Node | Daemon anonymous RSS | Cgroup file cache | Limit hits (`memory.events max`) |
| --- | ---: | ---: | ---: |
| node1 | 133Mi | 66Mi | 0 |
| node2 | 205Mi | 43Mi | 433 |
| node3 | 178Mi | 69Mi | 9 |

There were no OOM kills. Daemons had 25–28 threads and 64–65 file descriptors.
The node2 daemon mapped glibc and dozens of approximately 64Mi arena regions;
these virtual mappings are not themselves resident memory. Diagnostic files
occupied about 59–60Mi per pod and were rotating. The evidence points to
allocator retention/fragmentation as a contributor, rather than establishing
a leak of live allocations inside Cloudflare's closed-source daemon.

Applied a strategic merge patch to WARP alone: `MALLOC_ARENA_MAX=2`,
`MALLOC_TRIM_THRESHOLD_=131072`, memory request 256Mi, and limit 512Mi. Production
image pins and other containers were preserved because the local manifest's
pins were older than the running deployment. Extra memory is headroom, not a
leak fix. The first replacement had about 58Mi anonymous RSS, 106Mi total RSS,
and 251Mi virtual size; a SOCKS request to Cloudflare's trace returned `warp=on`.
Restarting also resets memory, so a fresh process alone does not prove the
long-term growth is resolved.

All three replacements became Ready with no restarts and returned `warp=on`
through their SOCKS proxies. Early samples showed approximately 58Mi anonymous
RSS per daemon and no cgroup limit hits; `kubectl top` reported 76–102Mi working
sets. `go test ./deploy/k8s` passed. A longer soak remains necessary.

## Verify memory growth

Inspect a running sidecar without reading its registration/config secrets:

```sh
kubectl top pods -n app -l app=gossip --containers
kubectl exec -n app POD -c warp -- sh -c '
  pid=$(pidof warp-svc)
  grep -E "VmSize:|VmRSS:|RssAnon:|RssFile:|Threads:" /proc/$pid/status
  tr "\000" "\n" < /proc/$pid/environ | grep "^MALLOC_"
  cat /sys/fs/cgroup/memory.current
  grep -E "^(anon|file|active_file|inactive_file) " /sys/fs/cgroup/memory.stat
  cat /sys/fs/cgroup/memory.events
  ls /proc/$pid/fd | wc -l
  du -sh /var/lib/cloudflare-warp
'
```

Compare anonymous memory on the same process across hours and representative
traffic. `kubectl top` includes active file cache; total cgroup usage additionally
includes inactive cache, which Linux can reclaim. Avoid treating virtual arena
reservations or rotating diagnostic files as proof of a heap leak. If anonymous
memory keeps climbing with stable traffic, collect a longer series and investigate
the vendor daemon; neither arena limits nor a higher container limit guarantees
that live allocations are released.
