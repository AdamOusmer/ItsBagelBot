<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import type { Snippet } from 'svelte';
  import '../styles/elements/chat-preview.css';
  import StatusDot from './StatusDot.svelte';

  let {
    title,
    label,
    kind,
    note,
    class: className = '',
    heading,
    children,
    ...rest
  }: Omit<HTMLAttributes<HTMLDivElement>, 'title' | 'class' | 'children'> & {
    title: string;
    label?: string;
    kind?: string;
    note?: string;
    class?: string;
    heading?: Snippet;
    children?: Snippet;
  } = $props();
</script>

<div class="bb-chat{className ? ' ' + className : ''}" role={label ? 'img' : 'group'} aria-label={label} {...rest}>
  <div class="bb-chat__bar">
    <StatusDot tone="success" />
    <span class="bb-chat__title">{#if heading}{@render heading()}{:else}{title}{/if}</span>
    {#if kind}<b class="bb-chat__kind">{kind}</b>{/if}
  </div>
  <div class="bb-chat__feed">{@render children?.()}</div>
  {#if note}<i class="gs-mark" data-note={note}></i>{/if}
</div>
