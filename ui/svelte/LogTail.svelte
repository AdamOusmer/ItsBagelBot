<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/log-tail.css';

  let {
    lines,
    label,
    max = 50,
    class: className = '',
    ...rest
  }: {
    lines: string[];
    label: string;
    max?: number;
    class?: string;
    [key: string]: unknown;
  } = $props();

  let box = $state<HTMLElement>();
  let pinned = true;

  const PIN_SLACK_PX = 8;

  const text = $derived(lines.slice(Math.max(0, lines.length - max)).join('\n'));

  function onscroll() {
    if (!box) return;
    pinned = box.scrollHeight - box.scrollTop - box.clientHeight < PIN_SLACK_PX;
  }

  $effect(() => {
    void text;
    if (box && pinned) box.scrollTop = box.scrollHeight;
  });

  const classes = $derived(['bb-log', className || null].filter(Boolean).join(' '));
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<pre
  class={classes}
  role="region"
  aria-label={label}
  tabindex="0"
  bind:this={box}
  {onscroll}
  {...rest}>{text}</pre>
