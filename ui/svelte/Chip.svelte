<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';

  let {
    on = false,
    onclick,
    type = 'button',
    tone = undefined,
    class: cls = '',
    children,
    ...rest
  }: {
    on?: boolean;
    onclick?: () => void;
    type?: 'button' | 'submit' | 'reset';
    tone?: 'muted' | 'danger' | 'eyebrow';
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();
</script>

<button
  {type}
  class="bb-chip{tone ? ` bb-chip--${tone}` : ''}{cls ? ` ${cls}` : ''}"
  data-on={on ? '' : undefined}
  {onclick}
  {...rest}
>{@render children()}</button>
