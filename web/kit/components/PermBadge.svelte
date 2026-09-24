<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import { getI18n } from '../lib/i18n/context';
  import { tPermBadge } from '../lib/module-copy';
  import type { Perm } from '../lib/types';

  let { perm }: { perm: Perm } = $props();
  const { t } = getI18n();

  const tone: Record<Perm, [string, string]> = {
    everyone: ['var(--bb-tier-inactive)', 'var(--bb-tier-inactive-border)'],
    sub: ['var(--bb-tier-paid)', 'var(--bb-tier-paid-border)'],
    vip: ['var(--bb-tier-vip)', 'var(--bb-tier-vip-border)'],
    mod: ['var(--bb-green-glow)', 'rgba(82,183,136,0.30)'],
    lead_mod: ['var(--bb-green-glow)', 'rgba(82,183,136,0.45)'],
    broadcaster: ['var(--bb-green-glow)', 'rgba(82,183,136,0.60)']
  };

  const swatch = $derived(tone[perm] ?? tone.everyone);
</script>

<Badge
  dashed={perm === 'lead_mod'}
  style="--badge-tone:{swatch[0]};--badge-tone-rule:{swatch[1]}"
>{tPermBadge(t, perm)}</Badge>
