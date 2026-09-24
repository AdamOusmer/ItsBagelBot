<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/brand-mark.css';

  let {
    title,
    sub,
    href,
    logoSrc,
    logoAlt = '',
    size = 'md',
    premium = false,
    class: className = '',
    ...rest
  }: {
    title: string;
    sub?: string;
    href?: string;
    logoSrc?: string;
    logoAlt?: string;
    size?: 'sm' | 'md' | 'lg';
    premium?: boolean;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-brand', `bb-brand--${size}`, className || null].filter(Boolean).join(' '),
  );

  const px = $derived(size === 'sm' ? 26 : size === 'lg' ? 55 : 30);
</script>

{#if href}
  <a class={classes} {href} data-premium={premium ? '' : undefined} {...rest}
    >{#if logoSrc}<span class="bb-brand__logo"
        ><img src={logoSrc} alt={logoAlt} width={px} height={px} /></span
      >{/if}<span class="bb-brand__id"
      ><span class="bb-brand__name">{title}</span>{#if sub}<span
          class="bb-brand__sub">{sub}</span
        >{/if}</span
    ></a
  >
{:else}
  <div class={classes} data-premium={premium ? '' : undefined} {...rest}
    >{#if logoSrc}<span class="bb-brand__logo"
        ><img src={logoSrc} alt={logoAlt} width={px} height={px} /></span
      >{/if}<span class="bb-brand__id"
      ><span class="bb-brand__name">{title}</span>{#if sub}<span
          class="bb-brand__sub">{sub}</span
        >{/if}</span
    ></div
  >
{/if}
