<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // In-page section navigation on the shared `.bb-tabs` rail. Real anchors,
  // never ARIA tabs: each one points at a section of this page, so the native
  // semantics (open in a new tab, right-click, the "link" role) are kept and
  // nothing has to reimplement roving focus.
  //
  // Which one is lit comes from the hash, via ../lib/hash-active.ts. The
  // server render marks none of them, which is correct rather than a
  // compromise: the server does not receive the fragment.
  import '../styles/elements/shell.css';
  import '../styles/tags.css';
  import { mountHashActive } from '../lib/hash-active';

  let {
    label,
    items,
    class: className = '',
    ...rest
  }: {
    label: string;
    items: { href: string; label: string; count?: number }[];
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-section-nav', 'bb-tabs', className || null].filter(Boolean).join(' '),
  );

  let navEl = $state<HTMLElement | null>(null);
  $effect(() => {
    if (!navEl) return;
    return mountHashActive(navEl);
  });
</script>

<div class="bb-section-nav-host"
  ><nav bind:this={navEl} class={classes} aria-label={label} {...rest}
    >{#each items as item (item.href)}<a class="bb-tab" href={item.href}
        >{item.label}{#if item.count != null}<span class="bb-section-nav__count"
            >{item.count}</span
          >{/if}</a
      >{/each}</nav
  ></div
>
