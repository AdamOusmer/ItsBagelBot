<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // A scroll region wearing the thin tan scrollbar instead of the browser's
  // default chrome bar. Drop it anywhere a panel needs to scroll: the account
  // menu's board list, the command inspector, a long popover.
  //
  // Extra attributes (role, aria-*, data-lenis-prevent) pass straight through
  // to the scrolling element -- `data-lenis-prevent` in particular has to land
  // on the element that actually scrolls or the smooth scroller eats the wheel
  // event and the panel never moves.
  import '../styles/elements/shell.css';
  import type { Snippet } from 'svelte';
  import type { HTMLAttributes } from 'svelte/elements';

  let {
    maxHeight,
    fill = false,
    padding,
    children,
    class: className = '',
    ...rest
  }: {
    /** Height cap before scrolling starts (e.g. '208px'). Omit to grow. */
    maxHeight?: string;
    /** Fill a flex parent and scroll the overflow. */
    fill?: boolean;
    /** Inner padding, kept inside the scroll region so the bar hugs the edge. */
    padding?: string;
    children: Snippet;
  } & HTMLAttributes<HTMLDivElement> = $props();

  const classes = $derived(
    ['bb-scroller', fill ? 'bb-scroller--fill' : null, 'bb-scroll', className || null]
      .filter(Boolean)
      .join(' '),
  );

  const style = $derived(
    [maxHeight && `max-height:${maxHeight}`, padding && `padding:${padding}`]
      .filter(Boolean)
      .join(';') || undefined,
  );
</script>

<div class={classes} {style} {...rest}>{@render children()}</div>
