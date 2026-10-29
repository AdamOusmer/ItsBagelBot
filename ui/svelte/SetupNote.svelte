<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import '../styles/elements/setup-step.css';
  import Card from './Card.svelte';
  import Text from './Text.svelte';

  let { title, body = '', icon }: {
    title: string;
    body?: string;
    icon?: Snippet;
  } = $props();
</script>

<Card class="bb-setup-note">
  {#if icon}<span class="bb-setup-note__icon" aria-hidden="true">{@render icon()}</span>{/if}
  <span class="bb-setup-note__copy">
    <Text as="span" size="sm"><strong>{title}</strong></Text>
    {#if body}<Text as="span" size="xs" tone="muted">{body}</Text>{/if}
  </span>
</Card>
