<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-tooltip` (../styles/elements/tooltip.css).
  // Astro twin: ../astro/Tooltip.astro.
  //
  // CSS-ONLY, NO POSITIONING ENGINE. floating-ui would flip the bubble away
  // from a viewport edge for 5-8 KB gzipped and a client runtime on every
  // surface that shows a hint, including the two that are otherwise static.
  // The trade taken is recorded at the contract: the bubble is centred over
  // its anchor, and the two places that genuinely sit against an edge (the
  // rail's collapsed labels, the dock) have their own labelled contracts and
  // do not use this.
  //
  // IT IS A HINT, NEVER THE ONLY LABEL. Hover hints are invisible to touch, so
  // anything a user MUST read to operate the control belongs in the accessible
  // name — which is why the bubble is aria-hidden and `describe` (opt-in) is
  // what wires it to the control with aria-describedby. Announced by default
  // it would be read twice for every control that also has a label.
  import '../styles/elements/tooltip.css';
  import type { Snippet } from 'svelte';

  let {
    text,
    placement = 'top',
    id,
    class: className = '',
    children,
    ...rest
  }: {
    text: string;
    placement?: 'top' | 'bottom';
    /**
     * Give the bubble an id and it stops being aria-hidden, so the wrapped
     * control can point at it with aria-describedby. Opt-in: see above.
     */
    id?: string;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-tooltip', placement === 'bottom' ? 'bb-tooltip--bottom' : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<span class={classes} {...rest}>{@render children()}<span
    class="bb-tooltip__bubble"
    {id}
    role={id ? 'tooltip' : undefined}
    aria-hidden={id ? undefined : 'true'}>{text}</span
  ></span>
