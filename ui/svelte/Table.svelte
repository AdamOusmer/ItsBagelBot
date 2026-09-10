<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-tbl` (../styles/elements/table.css).
  // Astro twin: ../astro/Table.astro.
  //
  // THE SCROLL WRAPPER IS PART OF THE ELEMENT and cannot be opted out of. A
  // <table> is the one element that refuses to shrink below its content: a
  // six-column table on a 375px phone makes the PAGE scroll sideways and takes
  // everything else on it along. Three console pages had rebuilt this wrapper
  // by hand and a fourth would have been the one to forget it.
  //
  // `role="region"` + `tabindex="0"` + a label on the wrapper are not
  // decoration either: a scroll container that cannot take focus cannot be
  // scrolled by keyboard, which fails WCAG 2.1.1 for exactly the users least
  // able to swipe it. `label` is therefore required — an unlabelled region is
  // announced as "region" and tells a screen-reader user nothing about which
  // of the page's three tables they have landed in.
  //
  // Rows are children, not a data prop. A `rows`/`columns` API would have to
  // grow a shape for a cell that is a link, a badge, a right-aligned number
  // and a colspan, all of which are already HTML.
  import '../styles/elements/table.css';
  import type { Snippet } from 'svelte';

  let {
    label,
    zebra = false,
    compact = false,
    class: className = '',
    children,
    ...rest
  }: {
    /** Accessible name for the scroll region. Required: see above. */
    label: string;
    zebra?: boolean;
    compact?: boolean;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-tbl',
      zebra ? 'bb-tbl--zebra' : null,
      compact ? 'bb-tbl--compact' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<!-- The rule is right in general and wrong here. It fires on `tabindex="0"`
     for any element it does not consider interactive, and a `role="region"`
     that SCROLLS is the documented exception: WCAG 2.1.1 requires a scroll
     container to be reachable by keyboard, and a container that cannot take
     focus cannot be scrolled by one. The ARIA Authoring Practices name this
     exact pattern (a labelled, focusable region around a wide table).
     Suppressed rather than worked around: the alternatives are dropping the
     tabindex (fails 2.1.1 for the users least able to swipe), or wrapping the
     table in a real control, which announces a button that does nothing. -->
<div class="bb-tbl-wrap" role="region" aria-label={label} tabindex="0"><table
    class={classes}
    {...rest}>{@render children()}</table
  ></div>
