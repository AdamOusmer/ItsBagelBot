<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

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
    maxHeight?: string;
    fill?: boolean;
    padding?: string;
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
