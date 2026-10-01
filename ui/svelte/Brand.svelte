<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import '../styles/elements/brand-mark.css';

  type Own = {
    title: string;
    sub?: string;
    href?: string;
    logoSrc?: string;
    logoAlt?: string;
    size?: 'sm' | 'md' | 'lg';
    logoShape?: 'square' | 'circle';
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
    fit = false,
    class: className = '',
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

  const classes = $derived(
    ['bb-brand', `bb-brand--${size}`, className || null].filter(Boolean).join(' '),
  );

  const px = $derived(size === 'sm' ? 26 : size === 'lg' ? 55 : 30);
</script>

{#if href}
  <a class={classes} {href} data-logo={logoShape === 'circle' ? 'circle' : undefined} {...rest}
    >{#if logoSrc}<span class="bb-brand__logo"
        ><img src={logoSrc} alt={logoAlt} width={px} height={px} /></span
      >{/if}<span class="bb-brand__id"
      ><span class="bb-brand__name">{title}</span>{#if sub}<span
          class="bb-brand__sub" data-fit={fit ? '' : undefined}>{sub}</span
        >{/if}</span
    ></a
  >
{:else}
  <div class={classes} data-logo={logoShape === 'circle' ? 'circle' : undefined} {...rest}
    >{#if logoSrc}<span class="bb-brand__logo"
        ><img src={logoSrc} alt={logoAlt} width={px} height={px} /></span
      >{/if}<span class="bb-brand__id"
      ><span class="bb-brand__name">{title}</span>{#if sub}<span
          class="bb-brand__sub" data-fit={fit ? '' : undefined}>{sub}</span
        >{/if}</span
    ></div
  >
{/if}
