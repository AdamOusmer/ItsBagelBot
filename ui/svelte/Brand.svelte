<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import '../styles/elements/brand-mark.css';
  import { splitWordmark } from '../lib/brand-wordmark';

  type Own = {
    title: string;
    sub?: string;
    href?: string;
    logoSrc?: string;
    logoAlt?: string;
    size?: 'sm' | 'md' | 'lg';
    logoShape?: 'square' | 'circle';
    wordmark?: boolean;
    fit?: boolean;
    class?: string;
  };

  let {
    title,
    sub,
    href,
    logoSrc,
    logoAlt = '',
    size = 'md',
    logoShape = 'square',
    wordmark = false,
    fit = false,
    class: className = '',
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

  const classes = $derived(
    ['bb-brand', `bb-brand--${size}`, wordmark ? 'bb-brand--wordmark' : null, className || null]
      .filter(Boolean)
      .join(' '),
  );

  const px = $derived(size === 'sm' ? 26 : size === 'lg' ? 55 : 30);
  const parts = $derived(wordmark && logoSrc ? splitWordmark(title) : null);
</script>

{#snippet body()}{#if logoSrc && !parts}<span class="bb-brand__logo"
      ><img src={logoSrc} alt={logoAlt} width={px} height={px} /></span
    >{/if}<span class="bb-brand__id"
    >{#if parts}<span class="bb-brand__name" role="img" aria-label={title}
        >{parts[0]}<img class="bb-brand__o" src={logoSrc} alt="" width={px} height={px} />{parts[1]}</span
      >{:else}<span class="bb-brand__name">{title}</span>{/if}{#if sub}<span
        class="bb-brand__sub" data-fit={fit ? '' : undefined}>{sub}</span
      >{/if}</span
  >{/snippet}

{#if href}
  <a class={classes} {href} data-logo={logoShape === 'circle' ? 'circle' : undefined} {...rest}
    >{@render body()}</a
  >
{:else}
  <div class={classes} data-logo={logoShape === 'circle' ? 'circle' : undefined} {...rest}
    >{@render body()}</div
  >
{/if}
