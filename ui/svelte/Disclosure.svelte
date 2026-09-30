<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/disclosure.css';
  import type { Snippet } from 'svelte';

  type Own = {
    summary: string;
    open?: boolean;
    index?: string;
    size?: 'md' | 'sm';
    class?: string;
    children?: Snippet;
  };

  let {
    summary,
    open = false,
    index,
    size = 'md',
    class: className = '',
    children,
    ...rest
  }: Own & Omit<SvelteHTMLElements['details'], keyof Own> = $props();

  const classes = $derived(
    [
      'bb-disclosure',
      size === 'sm' ? 'bb-disclosure--sm' : null,
      index ? 'bb-disclosure--indexed' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<details class={classes} {open} {...rest}><summary class="bb-disclosure__summary"
    >{#if index}<span class="bb-disclosure__index" aria-hidden="true">{index}</span>{/if}<span
      class="bb-disclosure__label">{summary}</span
    ><span class="bb-disclosure__icon" aria-hidden="true"></span></summary
  ><div class="bb-disclosure__body"><div class="bb-disclosure__content">{#if children}{@render children()}{/if}</div></div></details>
