<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { LaneView } from '#lib/server/lanes';
  import StatePill from '../StatePill.svelte';
  import { laneTone } from './lane-view';

  let {
    lane,
    selected = false,
    controls,
    onselect
  }: {
    lane?: LaneView;
    selected?: boolean;
    controls?: string;
    onselect?: (trigger: HTMLButtonElement) => void;
  } = $props();

  const { t, locale } = getI18n();
  const numbers = new Intl.NumberFormat(locale);
  const meta = $derived(lane ? t('admin.lanes.rowMeta', {
    consumer: lane.consumer,
    subject: lane.subject || '-'
  }) : '');
  const metrics = $derived.by(() => {
    if (!lane) return [];
    return [
      { term: 'admin.lanes.factPending', value: numbers.format(lane.pending), hot: lane.pending > 0 },
      { term: 'admin.lanes.factInFlight', value: lane.inFlight },
      { term: 'admin.lanes.factRate', value: lane.rate === '-' ? t('admin.lanes.sampling') : lane.rate },
      { term: 'admin.lanes.factDelivered', value: lane.delivered == null ? '-' : numbers.format(lane.delivered) },
      { term: 'admin.lanes.factRedelivered', value: numbers.format(lane.redelivered), hot: lane.redelivered > 0 }
    ];
  });

  function selectLane(event?: MouseEvent) {
    const trigger = event?.currentTarget;
    if (trigger instanceof HTMLButtonElement) onselect?.(trigger);
  }
</script>

<ManagementRow
  {selected}
  selectable={lane !== undefined}
  expanded={selected}
  controls={selected ? controls : undefined}
  onSelect={lane ? selectLane : undefined}
  aria-hidden={lane === undefined}
  style="box-sizing: border-box; block-size: var(--lane-row-size)"
>
  {#snippet primary()}
    <span class="row">
      {#if lane}
        <span class="identity">
          <StatusDot tone={laneTone(lane)} flat />
          <span class="nameplate">
            <span class="name" title={lane.display}>{lane.display}</span>
            <Text as="span" size="xs" tone="muted" mono truncate title={meta}>{meta}</Text>
          </span>
        </span>
        <span class="metrics">
          {#each metrics as metric (metric.term)}
            <span class="metric" class:hot={metric.hot}>
              <Text as="span" class="metric-label" size="xs" tone="muted">
                <bb-fit><span data-fit>{t(metric.term)}</span></bb-fit>
              </Text>
              <span class="value" title={metric.value}>
                <bb-fit><span data-fit>{metric.value}</span></bb-fit>
              </span>
            </span>
          {/each}
        </span>
        <span class="marks">
          <StatePill tone={lane.ephemeral ? 'paid' : 'free'}>
            <span class="badge-label">
              <bb-fit><span data-fit>{t(lane.ephemeral ? 'admin.lanes.ephemeral' : 'admin.lanes.durable')}</span></bb-fit>
            </span>
          </StatePill>
          <span class="orphan" class:visible={lane.orphan} aria-hidden={!lane.orphan}>
            <StatePill tone="banned">
              <span class="badge-label">
                <bb-fit><span data-fit>{t('admin.lanes.orphan')}</span></bb-fit>
              </span>
            </StatePill>
          </span>
        </span>
      {:else}
        <span class="identity">
          <Skeleton variant="pill" width="0.625rem" height="0.625rem" />
          <span class="nameplate">
            <Skeleton width="min(9rem, 90%)" height="10px" />
            <Skeleton width="min(14rem, 100%)" height="8px" />
          </span>
        </span>
        <span class="metrics">
          {#each Array(5) as _, index (index)}
            <span class="metric">
              <Skeleton width="min(9ch, 92%)" height="8px" />
              <Skeleton width="min(5ch, 60%)" height="8px" />
            </span>
          {/each}
        </span>
        <span class="marks">
          <Skeleton width="4.5rem" height="10px" />
          <Skeleton width="4.5rem" height="10px" />
        </span>
      {/if}
    </span>
  {/snippet}
</ManagementRow>

<style>
  .row {
    display: grid;
    grid-template-areas: "identity metrics marks";
    grid-template-columns: minmax(9rem, 1.3fr) minmax(0, 2.5fr) 9.75rem;
    align-items: center;
    gap: var(--bb-space-3);
    block-size: 100%;
    min-inline-size: 0;
  }

  .identity {
    grid-area: identity;
    display: flex;
    align-items: center;
    gap: var(--bb-space-2);
    min-inline-size: 0;
  }

  .nameplate {
    display: grid;
    flex: 1;
    grid-template-rows: repeat(2, 16px);
    align-items: center;
    min-inline-size: 0;
  }

  .name {
    overflow: hidden;
    color: var(--bb-white);
    font-size: var(--bb-text-sm);
    font-weight: 600;
    line-height: 16px;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .metrics {
    grid-area: metrics;
    display: grid;
    grid-template-columns: repeat(5, minmax(0, 1fr));
    gap: var(--bb-space-2);
  }

  .metric {
    display: grid;
    grid-template-rows: repeat(2, 16px);
    align-items: center;
    min-inline-size: 0;
  }

  .metric :global(.metric-label), .value {
    display: block;
    min-inline-size: 0;
    overflow: clip;
  }

  .value {
    color: var(--bb-white);
    font-family: var(--bb-font-mono);
    font-size: var(--bb-text-xs);
    font-variant-numeric: tabular-nums;
    line-height: 16px;
  }

  .metric.hot .value {
    color: var(--bb-tan-light);
  }

  .marks {
    grid-area: marks;
    display: flex;
    justify-content: end;
    align-items: center;
    gap: var(--bb-space-1);
  }

  .badge-label {
    inline-size: 4.5rem;
    text-align: center;
  }

  .orphan {
    display: flex;
    opacity: 0;
    transition: opacity var(--bb-dur-fast) ease;
  }

  .orphan.visible {
    opacity: 1;
  }

  @container lane-directory (width < 54rem) {
    .row {
      grid-template-areas:
        "identity marks"
        "metrics metrics";
      grid-template-columns: minmax(0, 1fr) 9.75rem;
      grid-template-rows: repeat(2, 32px);
      align-content: center;
      gap: 6px var(--bb-space-3);
    }
  }

  @container lane-directory (width < 28rem) {
    .row {
      grid-template-areas: "identity" "marks" "metrics";
      grid-template-columns: minmax(0, 1fr);
      grid-template-rows: 32px 20px 64px;
    }

    .marks {
      justify-content: start;
    }

    .metrics {
      grid-template-columns: repeat(3, minmax(0, 1fr));
      row-gap: 0;
    }
  }
</style>
