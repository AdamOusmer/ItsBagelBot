<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import type { Snippet } from 'svelte';
  import '../styles/elements/button.css';

  let {
    variant = 'primary',
    solid = false,
    danger = false,
    block = false,
    size = 'md',
    type = 'button',
    onclick,
    loading = false,
    done = false,
    disabled = false,
    class: cls = '',
    children,
    ...rest
  }: {
    variant?: 'primary' | 'secondary' | 'ghost' | 'green' | 'destructive' | 'icon' | 'tan' | 'quiet' | 'go';
    solid?: boolean;
    danger?: boolean;
    block?: boolean;
    size?: 'md' | 'sm';
    type?: 'button' | 'submit' | 'reset';
    onclick?: (e: MouseEvent) => void;
    loading?: boolean;
    done?: boolean;
    disabled?: boolean;
    class?: string;
    children?: Snippet;
    [key: string]: unknown;
  } = $props();

  $effect(() => {
    if (variant === 'icon' && !rest['aria-label'] && !rest['aria-labelledby']) {
      console.warn('[Button] variant="icon" needs an aria-label (icon-only button has no text).');
    }
  });

  const isDisabled = $derived(disabled || loading);

  const classes = $derived(
    [
      'bb-btn',
      `bb-btn--${variant}`,
      solid && 'bb-btn--solid',
      danger && 'bb-btn--danger-hover',
      block && 'bb-btn--block',
      size === 'sm' && 'bb-btn--sm',
      loading && 'is-loading',
      done && 'is-done',
      cls,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<button
  class={classes}
  {type}
  disabled={isDisabled}
  aria-busy={loading ? 'true' : undefined}
  data-mark=""
  {onclick}
  {...rest}
>
  {#if variant !== 'icon'}<i class="bb-btn__mark" aria-hidden="true"></i>{/if}
  <span class="bb-btn__content">{#if children}{@render children()}{/if}</span>
  {#if loading}<span class="bb-btn__spinner" aria-hidden="true"></span>{/if}
</button>
