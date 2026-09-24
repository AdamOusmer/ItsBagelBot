<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // A scroll region wearing the thin tan scrollbar instead of the browser's
  // default chrome bar. Drop it anywhere a panel needs to scroll: the account
  // menu's board list, the command inspector, a long popover.
  //
  // Extra attributes (role, aria-*, data-*) pass straight through to the
  // scrolling element. Nothing is needed for the smooth scroller: its
  // nested-scroll gate (lib/nested-scroll.ts) reads this pane's overflow and
  // position on every wheel and yields while the pane can move. Do NOT add
  // `data-lenis-prevent` here: it is static, so on a pane with nothing to
  // scroll it hands the wheel to a box that cannot move. It is for overlays
  // (a `<dialog>`, a modal card) that must never chain the wheel to the page.
  import '../styles/elements/shell.css';
  import type { Snippet } from 'svelte';
  import type { HTMLAttributes } from 'svelte/elements';

  let {
    maxHeight,
    fill = false,
    padding,
    smooth = false,
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
    /** Smooth-scroll this panel with its own Lenis (wheel only; touch stays native). */
    smooth?: boolean;
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

  let element: HTMLDivElement;

  // Imported on demand so lenis stays out of the console's entry chunk, the
  // same reason kit's initLenis is async.
  $effect(() => {
    if (!smooth) return;
    let destroy: (() => void) | undefined;
    let unmounted = false;
    void import('../lib/lenis').then(({ createPaneScroll }) => {
      if (!unmounted) destroy = createPaneScroll(element)?.destroy;
    });
    return () => {
      unmounted = true;
      destroy?.();
    };
  });
</script>

<div
  bind:this={element}
  class={classes}
  {style}
  {...rest}
>{@render children()}</div>
