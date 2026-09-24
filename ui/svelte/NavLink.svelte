<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/button.css';
  import '../styles/elements/nav-link.css';
  import type { Snippet } from 'svelte';

  let {
    href,
    label = '',
    current = false,
    variant = 'rail',
    external = false,
    block = false,
    disabled = false,
    hint,
    class: className = '',
    icon,
    trail,
    children,
    ...rest
  }: {
    href?: string;
    label?: string;
    current?: boolean;
    variant?: 'rail' | 'cta';
    external?: boolean;
    block?: boolean;
    disabled?: boolean;
    hint?: string;
    class?: string;
    icon?: Snippet;
    trail?: Snippet;
    children?: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-nav-link',
      variant === 'cta' ? 'bb-nav-link--cta' : null,
      block ? 'bb-nav-link--block' : null,
      variant === 'cta' ? 'bb-btn bb-btn--go-solid' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

{#if disabled}
  <span class={classes} aria-disabled="true" {...rest}
    >{#if icon}{@render icon()}{/if}<span class="bb-nav-link__label"
      >{#if children}{@render children()}{:else}{label}{/if}</span
    >{#if trail}{@render trail()}{/if}{#if hint}<span class="bb-nav-link__hint"
      >{hint}</span
    >{/if}</span
  >
{:else}
  <a
    class={classes}
    {href}
    aria-current={current ? 'page' : undefined}
    target={external ? '_blank' : undefined}
    rel={external ? 'noopener noreferrer' : undefined}
    {...rest}
    >{#if icon}{@render icon()}{/if}<span class="bb-nav-link__label"
      >{#if children}{@render children()}{:else}{label}{/if}</span
    >{#if trail}{@render trail()}{/if}</a
  >
{/if}
