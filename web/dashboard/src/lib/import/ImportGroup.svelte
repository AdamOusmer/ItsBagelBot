<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Eyebrow from '@bagel/ui/svelte/Eyebrow.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';

  let { title, count, children }: {
    title: string;
    count: number;
    children: Snippet;
  } = $props();
</script>

<Card flush>
  <div class="group-head">
    <Eyebrow>{title}</Eyebrow>
    <Text as="span" size="xs" mono tone="muted">{count}</Text>
  </div>
  {@render children()}
</Card>

<style>
  .group-head {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 16px 22px;
    background: var(--bb-elev-2);
  }
</style>
