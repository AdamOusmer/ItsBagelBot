<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import RankingCard from '@bagel/ui/svelte/RankingCard.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import AnalyticsFacts from './AnalyticsFacts.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { Bar, Fact, Name } from '#lib/analytics-page';

  let { id, bars, facts }: { id: string; bars?: Bar[]; facts?: Fact[] } = $props();
  const { t } = getI18n();
  const title = $derived(t(`admin.analytics.card.${id}`));
  const nameText = (name: Name) => 'text' in name ? name.text : t(name.key, name.params);
  const items = $derived((bars ?? []).map((bar) => ({
    id: bar.id,
    label: nameText(bar.name),
    value: bar.value,
    valueLabel: bar.valueText,
    secondary: bar.secondary ? t(bar.secondary.key, bar.secondary.params) : undefined
  })));
</script>

{#snippet factFooter()}
  {#if facts}<AnalyticsFacts items={facts} />{/if}
{/snippet}

<div class="ranking">
  <Scroller fill role="region" aria-label={title} aria-busy={!bars} tabindex={bars ? 0 : undefined} data-lenis-prevent>
    <RankingCard {title} headingLevel={3} description={t(`admin.analytics.hint.${id}`)} {items} compact style="min-block-size: 100%;" footer={facts?.length ? factFooter : undefined}>
      {#snippet empty()}
        {#if bars}
          <EmptyState title={t('admin.analytics.noRows')} />
        {:else}
          <div aria-hidden="true"><Skeleton variant="block" height="14rem" /></div>
        {/if}
      {/snippet}
    </RankingCard>
  </Scroller>
</div>

<style>
  .ranking {
    display: grid;
    grid-template: minmax(0, 1fr) / minmax(0, 1fr);
    min-inline-size: 0;
    block-size: 22rem;
  }
</style>
