<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import '../styles/elements/button.css';
  import '../styles/elements/nav-link.css';
  import type { Snippet } from 'svelte';

  type Own = {
    href?: string;
    label?: string;
    current?: boolean;
    variant?: 'rail' | 'cta';
    external?: boolean;
    block?: boolean;
    disabled?: boolean;
    hint?: string;
    fit?: boolean;
    fitGroup?: string;
    class?: string;
    leading?: Snippet;
    trailing?: Snippet;
    children?: Snippet;
  };

  let {
    href,
    label = '',
    current = false,
    variant = 'rail',
    external = false,
    block = false,
    disabled = false,
    hint,
    fit = false,
    fitGroup,
    class: className = '',
    leading,
    trailing,
    children,
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

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
    >{#if leading}{@render leading()}{/if}<span class="bb-nav-link__label" data-fit={fit ? '' : undefined} data-fit-group={fit ? fitGroup : undefined}
      >{#if children}{@render children()}{:else}{label}{/if}</span
    >{#if trailing}{@render trailing()}{/if}{#if hint}<span class="bb-nav-link__hint"
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
    >{#if leading}{@render leading()}{/if}<span class="bb-nav-link__label" data-fit={fit ? '' : undefined} data-fit-group={fit ? fitGroup : undefined}
      >{#if children}{@render children()}{:else}{label}{/if}</span
    >{#if trailing}{@render trailing()}{/if}</a
  >
{/if}
