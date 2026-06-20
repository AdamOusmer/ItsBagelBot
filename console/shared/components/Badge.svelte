<script lang="ts">
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

<span class="badge {perm}">{label[perm] ?? perm}</span>

<style>
  .badge { font-family: var(--bb-font-mono); font-size: 10px; letter-spacing: 0.08em; text-transform: uppercase; padding: 4px 10px; border-radius: var(--bb-radius-pill);
    display: inline-flex; align-items: center; gap: 5px; border: 1px solid transparent; }
  .badge.everyone { background: rgba(255,255,255,0.04); color: var(--bb-muted); border-color: var(--glass-border); }
  .badge.sub { background: rgba(201,168,124,0.10); color: var(--bb-tan-light); border-color: rgba(201,168,124,0.28); }
  .badge.vip { background: rgba(199,125,255,0.10); color: #d9aaff; border-color: rgba(199,125,255,0.30); }
  .badge.mod { background: rgba(82,183,136,0.10); color: var(--bb-green-glow); border-color: rgba(82,183,136,0.28); }
  .badge.lead_mod { background: rgba(82,183,136,0.14); color: var(--bb-green-glow); border-color: rgba(82,183,136,0.34); }
  .badge.broadcaster { background: rgba(82,183,136,0.16); color: var(--bb-green-glow); border-color: rgba(82,183,136,0.4); }
</style>
