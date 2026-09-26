<script module lang="ts">
  export interface RankingItem {
    id: string;
    label: string;
    href?: string;
    value: number;
    valueLabel: string;
    secondary?: string;
  }
</script>

<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import '../styles/elements/ranking-card.css';

  let {
    title, description = '', items, actions, leading,
    emptyLabel = 'No entries yet', class: cls = '', ...rest
  }: {
    title: string;
    description?: string;
    /** IDs are unique. Items render in the order supplied by the caller. */
    items: readonly RankingItem[];
    actions?: Snippet;
    leading?: Snippet<[RankingItem, number]>;
    emptyLabel?: string;
    class?: string;
    [key: string]: unknown;
  } = $props();
  const maximum = $derived(items.reduce((max, item) =>
    Number.isFinite(item.value) ? Math.max(max, item.value) : max, 0) || 1);
  function proportion(value: number): string {
    return `${Number.isFinite(value) ? Math.max(0, Math.min(100, value / maximum * 100)) : 0}%`;
  }
</script>

<section class={['bb-ranking-card', cls].filter(Boolean).join(' ')} aria-label={title} {...rest}>
  <header class="bb-ranking-card__head"><div class="bb-ranking-card__intro"><h2 class="bb-ranking-card__title">{title}</h2>{#if description}<p class="bb-ranking-card__description">{description}</p>{/if}</div>{#if actions}<div class="bb-ranking-card__actions">{@render actions()}</div>{/if}</header>
  {#if items.length}
    <ol class="bb-ranking-card__list">
      {#each items as item, index (item.id)}
        <li class="bb-ranking-card__row">
          <div class="bb-ranking-card__leading">
            <span class="bb-ranking-card__position">{String(index + 1).padStart(2, '0')}</span>
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
  {:else}
    <p class="bb-ranking-card__empty">{emptyLabel}</p>
  {/if}
</section>
