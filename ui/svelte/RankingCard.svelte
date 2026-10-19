<script module lang="ts">
  export interface RankingItem {
    id: string;
    label: string;
    href?: string;
    value: number;
    valueLabel: string;
    secondary?: string;
    position?: string;
  }
</script>

<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getUiI18n } from './i18n';
  import type { Snippet } from 'svelte';
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/ranking-card.css';

  const i18n = getUiI18n();
  let {
    title, headingLevel = 2, description = '', items, actions, leading, footer, empty, scale,
    emptyLabel = i18n.t('data.empty'), board = false, class: cls = '', ...rest
  }: {
    title: string;
    headingLevel?: 1 | 2 | 3 | 4 | 5 | 6;
    description?: string;
    /** IDs are unique. Items render in the order supplied by the caller. */
    items: readonly RankingItem[];
    actions?: Snippet;
    leading?: Snippet<[RankingItem, number]>;
    footer?: Snippet;
    empty?: Snippet;
    scale?: number;
    emptyLabel?: string;
    board?: boolean;
    class?: string;
  } & Omit<SvelteHTMLElements['section'], 'title' | 'class'> = $props();
  const maximum = $derived(scale || items.reduce((max, item) =>
    Number.isFinite(item.value) ? Math.max(max, item.value) : max, 0) || 1);
  function proportion(value: number): string {
    return `${Number.isFinite(value) ? Math.max(0, Math.min(100, value / maximum * 100)) : 0}%`;
  }
</script>

<section class={['bb-ranking-card', board && 'bb-ranking-card--board', cls].filter(Boolean).join(' ')} aria-label={title} {...rest}>
  <header class="bb-ranking-card__head"><div class="bb-ranking-card__intro"><svelte:element this={`h${headingLevel}`} class="bb-ranking-card__title">{title}</svelte:element>{#if description}<p class="bb-ranking-card__description">{description}</p>{/if}</div>{#if actions}<div class="bb-ranking-card__actions">{@render actions()}</div>{/if}</header>
  {#if items.length}
    <ol class="bb-ranking-card__list">
      {#each items as item, index (item.id)}
        <li class="bb-ranking-card__row">
          <div class="bb-ranking-card__leading">
            <span class="bb-ranking-card__position">{#if item.position}<span data-fit="">{item.position}</span>{:else}{String(index + 1).padStart(2, '0')}{/if}</span>
            {#if leading}<span class="bb-ranking-card__artwork" aria-hidden="true">{@render leading(item, index)}</span>{/if}
          </div>
          <div class="bb-ranking-card__content">
            <div class="bb-ranking-card__line">
              {#if item.href}<a class="bb-ranking-card__label" href={item.href}>{item.label}</a>{:else}<span class="bb-ranking-card__label">{item.label}</span>{/if}
              <strong class="bb-ranking-card__value">{item.valueLabel}</strong>
            </div>
            <div class="bb-ranking-card__track" aria-hidden="true"><span style={`width:${proportion(item.value)}`}></span></div>
            {#if item.secondary}<p class="bb-ranking-card__secondary">{item.secondary}</p>{/if}
          </div>
        </li>
      {/each}
    </ol>
  {:else if empty}
    <div class="bb-ranking-card__empty">{@render empty()}</div>
  {:else}
    <p class="bb-ranking-card__empty">{emptyLabel}</p>
  {/if}
  {#if footer}<div class="bb-ranking-card__footer">{@render footer()}</div>{/if}
</section>
