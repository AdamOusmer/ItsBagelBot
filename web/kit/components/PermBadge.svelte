<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import { getI18n } from '../lib/i18n/context';
  import { tPermBadge } from '../lib/module-copy';
  import type { Perm } from '../lib/types';

  let { perm }: { perm: Perm } = $props();
  const { t } = getI18n();

  const TONE: Record<Perm, 'inactive' | 'paid' | 'vip' | 'free'> = {
    everyone: 'inactive',
    sub: 'paid',
    vip: 'vip',
    mod: 'free',
    lead_mod: 'free',
    broadcaster: 'free'
  };
</script>

<Badge tone={TONE[perm] ?? 'inactive'} dashed={perm === 'lead_mod'}>{tPermBadge(t, perm)}</Badge>
