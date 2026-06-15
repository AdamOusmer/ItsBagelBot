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
