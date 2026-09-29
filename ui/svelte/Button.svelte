<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import type { Snippet } from 'svelte';
  import '../styles/elements/button.css';

  type Own = {
    variant?: 'primary' | 'secondary' | 'ghost' | 'green' | 'destructive' | 'icon' | 'tan' | 'quiet' | 'go' | 'add' | 'brand';
    as?: 'button' | 'span';
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
  };

  let {
    variant = 'primary',
    as = 'button',
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
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

  $effect(() => {
    if (variant === 'icon' && !rest['aria-label'] && !rest['aria-labelledby']) {
      console.warn('[Button] variant="icon" needs an aria-label (icon-only button has no text).');
    }
  });

  const isDisabled = $derived(disabled || loading);
  const marked = $derived(variant !== 'icon' && variant !== 'add');
  const isStatic = $derived(as === 'span');

  const classes = $derived(
    [
      'bb-btn',
      `bb-btn--${variant}`,
      isStatic && 'bb-btn--static',
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

  const elementProps = $derived(
    isStatic ? {} : { type, disabled: isDisabled, 'aria-busy': loading ? ('true' as const) : undefined, onclick },
  );
</script>

<svelte:element this={as} class={classes} {...elementProps} data-mark="" {...rest}>
  {#if marked}<i class="bb-btn__mark" aria-hidden="true"></i>{/if}
  <span class="bb-btn__content">{#if children}{@render children()}{/if}</span>
  {#if loading}<span class="bb-btn__spinner" aria-hidden="true"></span>{/if}
</svelte:element>
