# Rollout Guard

`smoke-and-rollback.sh` is the post-rollout guard for Flux/image updates.

It waits for the relevant Kubernetes rollouts, checks the public dashboard
health endpoint, and checks admin/dashboard `/readyz` from inside their pods.
By default it rolls every named workload back to the previous
ReplicaSet/DaemonSet revision if one of those checks fails.

Recommended use after Flux applies an image update:

```sh
./deploy/ops/smoke-and-rollback.sh
```

Keep this as an explicit post-deploy step rather than a blind cron rollback:
a transient public-network or New Relic location failure should page someone,
not automatically flap the whole fleet.
