<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit/i18n/context';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import FilterTabs from '#lib/components/FilterTabs.svelte';
  import type { LaneCategory, LanesResult } from '#lib/server/lanes';
  import type { filterLanes, PipelineStageFilter, PipelineTrafficFilter } from './lane-pipeline';

  let {
    result,
    filtered,
    delayed,
    categories,
    toggleCategory,
    onRefresh,
    search = $bindable(),
    stage = $bindable(),
    traffic = $bindable()
  }: {
    result: LanesResult | null;
    filtered: ReturnType<typeof filterLanes>;
    delayed: boolean;
    categories: ReadonlySet<LaneCategory>;
    toggleCategory: (value: LaneCategory | 'all') => void;
    onRefresh: () => void;
    search: string;
    stage: PipelineStageFilter;
    traffic: PipelineTrafficFilter;
  } = $props();

  const { t, locale } = getI18n();
  const nf = new Intl.NumberFormat(locale);
  const ready = $derived(result !== null && (!result.degraded || result.lanes.length > 0));

  const CATEGORY = {
    all: 'admin.lanes.categoryAll',
    system: 'admin.lanes.categorySystem',
    projection: 'admin.lanes.categoryProjection',
    ephemeral: 'admin.lanes.categoryEphemeral'
  } as const;
  const STAGE = {
    all: 'admin.lanes.pipelineAll',
    twitch: 'admin.lanes.pipelineTwitch',
    ingress: 'admin.lanes.pipelineIngress',
    outgress: 'admin.lanes.pipelineOutgress',
    system: 'admin.lanes.pipelineSystem'
  } as const;
  const TRAFFIC = {
    all: 'admin.lanes.trafficAll',
    stream: 'admin.lanes.trafficStream',
    standard: 'admin.lanes.trafficStandard',
    premium: 'admin.lanes.trafficPremium'
  } as const;

  function options<Value extends string>(labels: Record<Value, string>, counts: Record<Value, number>) {
    return (Object.keys(labels) as Value[]).map((value) => ({
      value,
      label: t(labels[value]),
      count: ready ? nf.format(counts[value]) : '\u00a0'
    }));
  }

  const trafficOptions = $derived(options(TRAFFIC, filtered.trafficCounts).map(({ value, label, count }) => ({
    value,
    label: ready ? `${label} (${count})` : label
  })));

  const summary = $derived.by(() => {
    const lanes = result?.lanes ?? [];
    return t('admin.lanes.stats', {
      lanes: nf.format(lanes.length),
      orphans: nf.format(lanes.filter((lane) => lane.orphan).length),
      pending: nf.format(lanes.reduce((total, lane) => total + lane.pending, 0))
    });
  });
</script>

<Stack gap={3} style="margin-block-end: var(--bb-space-4);">
  <FilterTabs
    label={t('admin.lanes.pipelineFilter')}
    options={options(STAGE, filtered.stageCounts)}
    value={stage}
    onValueChange={(value) => (stage = value as PipelineStageFilter)}
    countWidth="6ch"
  />

  <Cluster justify="between" gap={3}>
    <Cluster gap={2}>
      <div class="search">
        <SearchInput
          id="lane-search"
          fill
          tall
          bind:value={search}
          placeholder={t('admin.lanes.searchPlaceholder')}
          aria-label={t('admin.lanes.searchPlaceholder')}
        />
      </div>
      <div class="traffic">
        <Select
          aria-label={t('admin.lanes.trafficFilter')}
          options={trafficOptions}
          value={traffic}
          onchange={(event) => (traffic = event.currentTarget.value as PipelineTrafficFilter)}
          fill
        />
      </div>
    </Cluster>
    <Cluster gap={2}>
      <div class="summary-text" aria-busy={result === null}>
        {#if ready}
          <Text as="div" size="xs" tone="muted" mono>
            <bb-fit><span data-fit>{summary}</span></bb-fit>
          </Text>
        {:else if result === null}
          <Skeleton width="min(100%, 20ch)" height="1rem" />
        {/if}
      </div>
      <Cluster gap={2} nowrap>
        <div class="live-status" class:visible={result !== null} aria-hidden={result === null}>
          <Tag tone={delayed ? 'warning' : 'live'} bare status style="display: flex;">
            <bb-fit><span data-fit>{t(delayed ? 'admin.lanes.delayed' : 'admin.lanes.live')}</span></bb-fit>
          </Tag>
        </div>
        <Button variant="ghost" onclick={onRefresh} disabled={result === null} --btn-w="7rem">
          <bb-fit><span data-fit>{t('admin.lanes.refresh')}</span></bb-fit>
        </Button>
      </Cluster>
    </Cluster>
  </Cluster>

  <Stack gap={1}>
    <Label as="span" id="lane-category-filter"><bb-fit><span data-fit>{t('admin.lanes.categoryFilter')}</span></bb-fit></Label>
    <div class="categories" role="group" aria-labelledby="lane-category-filter">
      {#each options(CATEGORY, filtered.categoryCounts) as option (option.value)}
        <Chip
          pressed={option.value === 'all' ? categories.size === 0 : categories.has(option.value)}
          onclick={() => toggleCategory(option.value)}
          style="min-block-size: var(--btn-min-h); min-inline-size: 0; justify-content: space-between;"
        >
          <span class="category-label"><bb-fit><span data-fit>{option.label}</span></bb-fit></span>
          <span class="category-count">{option.count}</span>
        </Chip>
      {/each}
    </div>
  </Stack>
</Stack>

<style>
  .search { inline-size: 15rem; max-inline-size: 100%; }
  .traffic { inline-size: 11rem; max-inline-size: 100%; }

  .summary-text {
    display: grid;
    align-items: center;
    inline-size: 17rem;
    max-inline-size: 100%;
    block-size: 1.25rem;
  }

  .live-status {
    display: grid;
    inline-size: 8rem;
    opacity: 0;
    transition: opacity var(--bb-dur-base) var(--bb-ease-out-expo);
  }

  .live-status.visible {
    opacity: 1;
  }

  .categories {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 7.5rem), 1fr));
    gap: var(--bb-space-2);
  }

  .category-label { min-inline-size: 0; }

  .category-count {
    flex: none;
    min-inline-size: 6ch;
    text-align: end;
    font-variant-numeric: tabular-nums;
  }
</style>
