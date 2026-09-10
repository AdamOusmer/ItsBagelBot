<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // A scroll region wearing the console's thin tan scrollbar instead of the
  // browser's default chrome bar. Drop it anywhere a panel needs to scroll:
  // the account menu's shared-dashboards list, the command inspector, and so on.
  // Extra attributes (role, aria-*, data-lenis-prevent, …) pass straight through
  // to the scrolling element.
  import type { Snippet } from 'svelte';
  import type { HTMLAttributes } from 'svelte/elements';

  let {
    maxHeight,
    fill = false,
    padding,
    children,
    ...rest
  }: {
    // Height cap before scrolling kicks in (e.g. '208px'). Omit to grow freely.
    maxHeight?: string;
    // Fill a flex parent and scroll the overflow (flex:1; min-height:0).
    fill?: boolean;
    // Inner padding kept inside the scroll region so the bar hugs the edge.
    padding?: string;
    children: Snippet;
  } & HTMLAttributes<HTMLDivElement> = $props();

  const style = $derived(
    [maxHeight && `max-height:${maxHeight}`, padding && `padding:${padding}`]
      .filter(Boolean)
      .join(';') || undefined
  );
</script>

<div class="scroller bb-scroll" class:fill {style} {...rest}>
  {@render children()}
</div>

<style>
  /* The thin tan bar itself is .bb-scroll in @bagel/ui/styles/a11y.css; the
     literal rgba(201, 168, 124, 0.35) this file used to repeat twice IS
     --bb-border-strong, which is what that rule defaults to. Only the 6px
     width is this component's: a panel bar inside a card has to read as
     narrower than the page's. */
  .scroller {
    overflow-y: auto;
    overscroll-behavior: contain;
    --bb-scrollbar-size: 6px;
  }
  .scroller.fill { flex: 1; min-height: 0; }
</style>
