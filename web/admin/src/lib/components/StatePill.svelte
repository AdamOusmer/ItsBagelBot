<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import type { Snippet } from 'svelte';

  type RoleTone = 'moderator' | 'admin' | 'owner';
  type BadgeTone =
    | 'free'
    | 'paid'
    | 'vip'
    | 'banned'
    | 'inactive'
    | 'neutral'
    | 'positive'
    | 'warning'
    | 'danger';

  let {
    tone,
    shape = 'tag',
    children
  }: {
    shape?: 'tag' | 'pill';
    tone: BadgeTone | RoleTone;
    children: Snippet;
  } = $props();

  const ROLE_TONE: Record<RoleTone, BadgeTone> = { moderator: 'neutral', admin: 'paid', owner: 'vip' };

  const isRole = (value: BadgeTone | RoleTone): value is RoleTone => value in ROLE_TONE;

  const badgeTone = $derived(isRole(tone) ? ROLE_TONE[tone] : tone);
</script>

<Badge {shape} tone={badgeTone}>{@render children()}</Badge>
