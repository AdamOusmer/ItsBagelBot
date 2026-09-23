<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-log` contract
  // (../styles/elements/log-tail.css). Svelte only: see
  // SINGLE_ADAPTER_REASON in ../scripts/gen-catalog.mjs.
  //
  // The newest line is the one a person came for, so the box keeps itself
  // scrolled to the bottom as lines arrive, until the reader scrolls up to
  // look at something; it re-pins when they scroll back down. Without the
  // second half, a tail that is still being fed yanks the reader back to the
  // bottom mid-read on every snapshot.
  import '../styles/elements/log-tail.css';

  let {
    lines,
    label,
    max = 50,
    class: className = '',
    ...rest
  }: {
    lines: string[];
    /** Accessible name of the scroll region, e.g. the job or pod it came from. */
    label: string;
    /** Keep only the last `max` lines. */
    max?: number;
    class?: string;
    [key: string]: unknown;
  } = $props();

  let box = $state<HTMLElement>();
  // Plain, not $state: it is read inside the effect but must not re-run it.
  // A scroll is not a reason to jump to the bottom; a new line is.
  let pinned = true;

  // 8px of slack: a fractional scrollTop under browser zoom never reaches
  // scrollHeight - clientHeight exactly, and a reader parked at the bottom
  // must stay pinned.
  const PIN_SLACK = 8;

  const text = $derived(lines.slice(Math.max(0, lines.length - max)).join('\n'));

  function onscroll() {
    if (!box) return;
    pinned = box.scrollHeight - box.scrollTop - box.clientHeight < PIN_SLACK;
  }

  $effect(() => {
    void text;
    if (box && pinned) box.scrollTop = box.scrollHeight;
  });

  const classes = $derived(['bb-log', className || null].filter(Boolean).join(' '));
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<!-- Same exception Table.svelte documents: a labelled region that SCROLLS has
     to take keyboard focus (WCAG 2.1.1), or it cannot be scrolled without a
     pointer. role="region" and not role="log": a log is a polite live region,
     and this box is re-rendered from a fresh snapshot, so every frame would
     re-announce up to fifty lines to a screen reader user who asked for none
     of them. -->
<pre
  class={classes}
  role="region"
  aria-label={label}
  tabindex="0"
  bind:this={box}
  {onscroll}
  {...rest}>{text}</pre>
