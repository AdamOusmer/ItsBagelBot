<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import { getUiI18n } from './i18n';
  import type { Snippet } from 'svelte';
  import type { CardTone } from '../lib/tone';
  import '../styles/elements/community-card.css';

  const i18n = getUiI18n();
  type Own = {
    title: string;
    subtitle?: string;
    total: string;
    period?: string;
    tone?: CardTone;
    appearance?: 'solid' | 'soft';
    artwork?: Snippet;
    /** Caller-owned details or contributor content below the cover. */
    children?: Snippet;
    class?: string;
  };

  let {
    title, subtitle = '', total, period = i18n.t('data.allTime'), tone = 'warm',
    appearance = 'soft', artwork, children, class: cls = '', ...rest
  }: Own & Omit<SvelteHTMLElements['section'], keyof Own> = $props();
  const classes = $derived([
    'bb-community-card', `bb-community-card--${tone}`, `bb-community-card--${appearance}`, cls,
  ].filter(Boolean).join(' '));
</script>

<section class={classes} aria-label={title} {...rest}>
  <div class="bb-community-card__cover">
    <div class="bb-community-card__head"><h2 class="bb-community-card__title">{title}</h2>{#if subtitle}<p class="bb-community-card__subtitle">{subtitle}</p>{/if}</div>
    {#if artwork}<div class="bb-community-card__artwork" aria-hidden="true">{@render artwork()}</div>{/if}
    <div class="bb-community-card__measure"><strong class="bb-community-card__total">{total}</strong>{#if period}<p class="bb-community-card__period">{period}</p>{/if}</div>
  </div>
  {#if children}<div class="bb-community-card__body">{@render children()}</div>{/if}
</section>
