<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import type { Perm } from '../lib/types';
  let { perm }: { perm: Perm } = $props();
  const label: Record<Perm, string> = {
    everyone: 'Everyone',
    sub: 'Subs',
    vip: 'VIPs',
    mod: 'Mods',
    lead_mod: 'Lead Mods',
    broadcaster: 'Broadcaster'
  };
</script>

<span class="bb-tag t-{perm}">{label[perm] ?? perm}</span>

<style>
  /* Was a tinted pill per permission (a duplicate of the global .badge rules).
     Now the global Tier-1 label (.bb-tag); the only thing left here is the
     perm ladder's tone, which the shared vocabulary has no modifier for.
     House palette: steel everyone, tan subs, SILVER VIP (it shipped purple
     #d9aaff by mistake, VIP is never purple here), green by mod authority. */
  .t-everyone { color: var(--bb-fg-2); border-bottom-color: rgba(201,168,124,0.20); }
  .t-sub { color: var(--bb-tan-light); border-bottom-color: rgba(201,168,124,0.40); }
  .t-vip { color: #dfe4e9; border-bottom-color: rgba(217,222,228,0.34); }
  .t-mod { color: var(--bb-green-glow); border-bottom-color: rgba(82,183,136,0.30); }
  .t-lead_mod { color: var(--bb-green-glow); border-bottom-style: dashed; border-bottom-color: rgba(82,183,136,0.45); }
  .t-broadcaster { color: var(--bb-green-glow); border-bottom-color: rgba(82,183,136,0.60); }
</style>
