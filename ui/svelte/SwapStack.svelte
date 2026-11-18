<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/swap-stack.css';
  import type { Snippet } from 'svelte';

  type Own = {
    alternate: boolean;
    primary: Snippet;
    secondary: Snippet;
    class?: string;
  };

  let { alternate, primary, secondary, class: className = '', ...rest }: Own & Omit<SvelteHTMLElements['div'], keyof Own> =
    $props();
</script>

<div class={['bb-swap-stack', className]} {...rest}
  ><div class={['bb-swap-stack__layer', !alternate && 'bb-swap-stack__layer--shown']} inert={alternate}>{@render primary()}</div
  ><div class={['bb-swap-stack__layer', alternate && 'bb-swap-stack__layer--shown']} inert={!alternate}>{@render secondary()}</div
></div>
