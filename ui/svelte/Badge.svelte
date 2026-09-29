<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import '../styles/tags.css';
  import '../styles/elements/badge.css';

  let {
    tone = undefined,
    shape = 'tag',
    mark = undefined,
    sweep = false,
    dashed = false,
    literal = false,
    status = false,
    children,
    class: className = '',
    ...rest
  }: {
    shape?: 'tag' | 'pill';
    tone?: 'quiet' | 'live' | 'alpha' | 'pre' | 'incoming' | 'bare' | 'free' | 'paid' | 'vip' | 'banned' | 'inactive' | 'neutral' | 'positive' | 'warning' | 'danger';
    mark?: 'solid' | 'hollow' | 'dash' | 'up' | 'plus';
    sweep?: boolean;
    dashed?: boolean;
    literal?: boolean;
    status?: boolean;
    children: Snippet;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-tag bb-badge',
      shape === 'pill' ? 'bb-badge--pill' : null,
      tone ? `bb-tag--${tone}` : null,
      dashed ? 'bb-badge--dashed' : null,
      literal ? 'bb-tag--literal' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<span
  class={classes}
  role={status ? 'status' : undefined}
  {...rest}
>{#if mark}<i class="bb-mark{mark === 'solid' ? '' : ` bb-mark--${mark}`}" aria-hidden="true"></i>{/if}{@render children()}{#if sweep}<i class="bb-sweep" aria-hidden="true"></i>{/if}</span>
