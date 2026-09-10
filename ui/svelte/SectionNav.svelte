<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Thin adapter over THE RAIL (`.bb-tabs`, ../styles/tags.css): in-page
  // section navigation. Real anchors, never ARIA tabs -- each one points at a
  // section of this page, so the native semantics (open in a new tab,
  // right-click, the "link" role) are kept and nothing has to reimplement
  // roving focus.
  //
  // Everything this element used to declare for itself -- the container
  // query, the count slot, the active marker -- is the rail contract now, so
  // a section jump and a page filter are the same box. The `.bb-section-nav`
  // block that carried it lived in ../styles/elements/shell.css.
  //
  // Which one is lit comes from the hash, via ../lib/hash-active.ts. The
  // server render marks none of them, which is correct rather than a
  // compromise: the server does not receive the fragment.
  import '../styles/tags.css';
  import { mountHashActive } from '../lib/hash-active';

  let {
    label,
    items,
    orientation = 'auto',
    class: className = '',
    ...rest
  }: {
    label: string;
    items: { href: string; label: string; count?: number }[];
    /**
     * `auto` is a row while its column is wide enough for one and a vertical
     * hairline rail once it is not -- a container query, so the same markup
     * serves a phone and a two-column settings page. The two fixed values
     * exist for a caller whose column cannot change width.
     */
    orientation?: 'auto' | 'horizontal' | 'vertical';
    class?: string;
    [key: string]: unknown;
  } = $props();

  const MODIFIER = {
    auto: 'bb-tabs--auto',
    horizontal: 'bb-tabs--wrap',
    vertical: 'bb-tabs--vertical',
  } as const;

  const classes = $derived(
    ['bb-tabs', MODIFIER[orientation], className || null].filter(Boolean).join(' '),
  );

  let navEl = $state<HTMLElement | null>(null);
  $effect(() => {
    if (!navEl) return;
    return mountHashActive(navEl);
  });
</script>

<div class="bb-tabs-host"
  ><nav bind:this={navEl} class={classes} aria-label={label} {...rest}
    >{#each items as item (item.href)}<a class="bb-tab" href={item.href}
        >{item.label}{#if item.count != null}<span class="bb-tab__count"
            >{item.count}</span
          >{/if}</a
      >{/each}</nav
  ></div
>
