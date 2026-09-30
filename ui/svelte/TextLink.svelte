<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/text-link.css';
  import type { Snippet } from 'svelte';
  import type { IconName } from '../lib/icons';
  import Icon from './Icon.svelte';

  type Own = {
    href: string;
    label?: string;
    variant?: 'roll' | 'arrow' | 'inline' | 'quiet';
    tone?: 'go' | 'lead';
    prose?: boolean;
    icon?: IconName;
    active?: boolean;
    external?: boolean;
    size?: string;
    touch?: boolean;
    class?: string;
    children?: Snippet;
  };

  let {
    href,
    label = '',
    variant = 'roll',
    tone,
    prose = false,
    icon,
    active = false,
    external = false,
    size,
    touch = false,
    class: className = '',
    children,
    ...rest
  }: Own & Omit<SvelteHTMLElements['a'], keyof Own> = $props();

  const ROOT = {
    roll: 'bb-text-link',
    arrow: 'bb-link bb-link--arrow',
    inline: 'bb-link bb-link--inline',
    quiet: 'bb-link bb-link--quiet',
  } as const;

  const arrow = $derived(variant === 'arrow');
  const glyphs = $derived(Array.from(label));
  const classes = $derived(
    [
      ROOT[variant],
      arrow && tone ? `bb-link--${tone}` : null,
      arrow && prose ? 'bb-link--prose' : null,
      touch && variant === 'quiet' ? 'bb-link--touch' : null,
      className || null,
      active ? 'is-active' : null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

{#if variant === 'roll'}<a
    class={classes}
    {href}
    aria-label={label}
    aria-current={active ? 'page' : undefined}
    target={external ? '_blank' : undefined}
    rel={external ? 'noopener noreferrer' : undefined}
    style={size ? `--text-link-size: ${size};` : undefined}
    {...rest}
    >{#if icon}<Icon name={icon} size={14} class="bb-text-link__icon" />{/if}<span
      class="bb-text-link__mask"
      aria-hidden="true"
      ><span class="bb-text-link__row bb-text-link__row--rest"
        >{#each glyphs as glyph, index}<span class="bb-text-link__glyph" style="--gi: {index};"
            >{glyph}</span
          >{/each}</span
      ><span class="bb-text-link__row bb-text-link__row--over"
        >{#each glyphs as glyph, index}<span class="bb-text-link__glyph" style="--gi: {index};"
            >{glyph}</span
          >{/each}</span
      ></span
    ></a
  >{:else}<a
    class={classes}
    {href}
    aria-current={active ? 'page' : undefined}
    target={external ? '_blank' : undefined}
    rel={external ? 'noopener noreferrer' : undefined}
    {...rest}
    >{#if icon && variant === 'quiet'}<Icon
        name={icon}
        size={14}
        class="bb-link__icon"
      />{/if}{#if children}{@render children()}{:else}{label}{/if}{#if arrow}<span
        class="bb-link__arrow"
        aria-hidden="true">→</span
      >{/if}</a
  >{/if}
