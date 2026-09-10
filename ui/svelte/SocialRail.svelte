<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-social-rail`. Its Astro twin is
  // ../astro/SocialRail.astro; ../test/parity.test.ts diffs the two.
  //
  // The five destinations that used to be a `const socials` array inside this
  // component are the bot's accounts, so they live with the bot
  // (web/kit/lib/social.ts) and arrive as `items`. What stays here is the only
  // part that is design: a corner stack of brand marks that fades up on hover
  // and leaves under 900px.
  import '../styles/elements/nav.css';
  import Icon from './Icon.svelte';
  import type { IconName } from '../lib/icons';

  let {
    items,
    ariaLabel,
    size = 17,
    class: className = '',
    ...rest
  }: {
    items: { label: string; href: string; icon: IconName }[];
    ariaLabel: string;
    /** Glyph edge. 17px is the measured size of the corner stack. */
    size?: number;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-social-rail', className || null].filter(Boolean).join(' '));
</script>

<aside class={classes} aria-label={ariaLabel} {...rest}
  >{#each items as item (item.href)}<a
      class="bb-social-rail__item"
      href={item.href}
      aria-label={item.label}
      title={item.label}
      target="_blank"
      rel="noopener noreferrer"><Icon name={item.icon} {size} /></a
    >{/each}</aside
>
