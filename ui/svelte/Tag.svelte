<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import '../styles/tags.css';
  import type { Snippet } from 'svelte';
  import type { TagTone } from '../lib/tone';

  type Own = {
    tone?: TagTone;
    bare?: boolean;
    literal?: boolean;
    mark?: 'solid' | 'hollow' | 'dash' | 'up' | 'plus';
    sweep?: boolean;
    status?: boolean;
    as?: 'span' | 'small' | 'div';
    class?: string;
    children: Snippet;
  };

  let {
    tone = undefined,
    bare = false,
    literal = false,
    mark = undefined,
    sweep = false,
    status = false,
    as: tag = 'span',
    class: className = '',
    children,
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

  const classes = $derived(
    [
      'bb-tag',
      tone ? `bb-tag--${tone}` : null,
      bare ? 'bb-tag--bare' : null,
      literal ? 'bb-tag--literal' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element this={tag} class={classes} role={status ? 'status' : undefined} {...rest}
  >{#if mark}<i
      class="bb-mark{mark === 'solid' ? '' : ` bb-mark--${mark}`}"
      aria-hidden="true"
    ></i>{/if}{@render children()}{#if sweep}<i class="bb-sweep" aria-hidden="true"></i>{/if}</svelte:element
>
