<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import '../styles/elements/chat-preview.css';
  import StatusDot from './StatusDot.svelte';

  let {
    title,
    label,
    kind,
    note,
    class: className = '',
    onpointerenter,
    onpointerleave,
    children
  }: {
    title: string;
    label?: string;
    kind?: string;
    note?: string;
    class?: string;
    onpointerenter?: (event: PointerEvent) => void;
    onpointerleave?: (event: PointerEvent) => void;
    children?: Snippet;
  } = $props();
</script>

<div class="bb-chat{className ? ' ' + className : ''}" role={label ? 'img' : 'group'} aria-label={label} {onpointerenter} {onpointerleave}>
  <div class="bb-chat__bar">
    <StatusDot tone="success" />
    <span class="bb-chat__title">{title}</span>
    {#if kind}<b class="bb-chat__kind">{kind}</b>{/if}
  </div>
  <div class="bb-chat__feed">{@render children?.()}</div>
  {#if note}<i class="gs-mark" data-note={note}></i>{/if}
</div>
