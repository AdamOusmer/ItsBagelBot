<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit/i18n/context';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { LanesResult, LaneView } from '#lib/server/lanes';
  import LaneRow from './LaneRow.svelte';
  import { groupLanes, laneKey } from './lane-view';

  let {
    result,
    rows,
    selectedId,
    controls,
    onSelect
  }: {
    result: LanesResult | null;
    rows: LaneView[];
    selectedId: string | null;
    controls: string;
    onSelect: (lane: LaneView, trigger: HTMLButtonElement) => void;
  } = $props();

  const { t, locale } = getI18n();
  const nf = new Intl.NumberFormat(locale);
  const groups = $derived(groupLanes(rows));
</script>

<DeckList
  style="container: lane-directory / inline-size;"
  role="region"
  aria-label={t('admin.lanes.listLabel')}
  aria-busy={result === null}
>
  <div class="directory">
    {#if result === null}
      <div role="status" aria-label={t('common.loading')}>
        <div class="stream-heading" aria-hidden="true">
          <Skeleton width="12ch" />
          <Skeleton width="min(15ch, 100%)" />
        </div>
        {#each Array(4) as _, index (index)}
          <LaneRow />
        {/each}
      </div>
    {:else if result.degraded && !result.lanes.length}
      <div class="directory-notice">
        <AlertBanner tone="warning" role="status">
          {t('admin.lanes.emptyDegraded')}
        </AlertBanner>
      </div>
    {:else if groups.length}
      {#each groups as group (group.stream)}
        <section class="stream">
          <div class="stream-heading">
            <Heading level={2} variant="eyebrow">
              <bb-fit><span data-fit>{group.stream}</span></bb-fit>
            </Heading>
            <Text as="span" size="xs" tone="muted" mono style="text-align: end;">
              <bb-fit><span data-fit>{t('admin.lanes.groupSummary', {
                lanes: nf.format(group.lanes.length),
                pending: nf.format(group.pending)
              })}</span></bb-fit>
            </Text>
          </div>
          <ul class="bb-list" role="list" aria-label={group.stream}>
            {#each group.lanes as lane (laneKey(lane))}
              <li>
                <LaneRow
                  {lane}
                  selected={selectedId === laneKey(lane)}
                  {controls}
                  onselect={(trigger) => onSelect(lane, trigger)}
                />
              </li>
            {/each}
          </ul>
        </section>
      {/each}
    {:else if result.lanes.length}
      <EmptyState title={t('admin.lanes.emptyMatch')} body={t('admin.lanes.emptyMatchBody')} />
    {:else}
      <EmptyState title={t('admin.lanes.empty')} body={t('admin.lanes.emptyBody')} />
    {/if}
  </div>
</DeckList>

<style>
  .directory {
    --lane-row-size: 58px;
    min-block-size: calc(44px + 4 * var(--lane-row-size));
  }

  .directory-notice { padding: var(--bb-space-3); }

  .stream + .stream { margin-block-start: var(--bb-space-3); }

  .stream-heading {
    display: grid;
    align-items: center;
    padding-inline: var(--bb-space-4);
    gap: var(--bb-space-3);
    block-size: 44px;
    grid-template-columns: minmax(0, 1fr) minmax(0, 10rem);
  }

  @container lane-directory (width < 54rem) {
    .directory { --lane-row-size: 96px; }
  }

  @container lane-directory (width < 28rem) {
    .directory { --lane-row-size: 154px; }
    .stream-heading { grid-template-columns: minmax(0, 1fr) minmax(0, 8rem); }
  }
</style>
