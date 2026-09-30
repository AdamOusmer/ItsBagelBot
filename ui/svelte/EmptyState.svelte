<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/empty-state.css';
  import type { Snippet } from 'svelte';

  type Own = {
    title: string;
    body?: string;
    children?: Snippet;
  };

  let {
    title,
    body = undefined as string | undefined,
    children = undefined as Snippet | undefined,
    class: className,
    ...rest
  }: Own & Omit<SvelteHTMLElements['div'], keyof Own> = $props();
</script>

<div class={['bb-empty', className]} {...rest}>
  <p class="bb-empty__title">{title}</p>
  {#if body}<p class="bb-empty__body">{body}</p>{/if}
  {#if children}<div class="bb-empty__cta">{@render children()}</div>{/if}
</div>
