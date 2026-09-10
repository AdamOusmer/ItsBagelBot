<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  //
  // "Nothing here yet" panel: a title, optional body copy, and an optional CTA
  // row the caller fills with buttons.
  import type { Snippet } from 'svelte';

  let {
    title,
    body = undefined as string | undefined,
    children = undefined as Snippet | undefined,
    ...rest
  }: {
    title: string;
    body?: string;
    children?: Snippet;
    [key: string]: unknown;
  } = $props();
</script>

<div class="bb-empty" {...rest}>
  <p class="bb-empty__title">{title}</p>
  {#if body}<p class="bb-empty__body">{body}</p>{/if}
  {#if children}<div class="bb-empty__cta">{@render children()}</div>{/if}
</div>
