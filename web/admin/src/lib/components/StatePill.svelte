<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import type { ComponentProps, Snippet } from 'svelte';

  type BadgeTone = NonNullable<ComponentProps<typeof Badge>['tone']>;
  const ROLE_TONE = { moderator: 'neutral', admin: 'paid', owner: 'vip' } as const;
  type RoleTone = keyof typeof ROLE_TONE;

  let {
    tone,
    shape = 'tag',
    children
  }: {
    shape?: 'tag' | 'pill';
    tone: BadgeTone | RoleTone;
    children: Snippet;
  } = $props();

  function isRole(value: BadgeTone | RoleTone): value is RoleTone {
    return Object.hasOwn(ROLE_TONE, value);
  }

  const badgeTone = $derived(isRole(tone) ? ROLE_TONE[tone] : tone);
</script>

<Badge {shape} tone={badgeTone}>{@render children()}</Badge>
