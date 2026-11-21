<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { invalidate } from '$app/navigation';
  import { getAbortSignal } from 'svelte';
  import { SvelteSet } from 'svelte/reactivity';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { livePoll } from '@bagel/kit/live-poll';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import { allows } from '#lib/access';
  import LaneToolbar from '#lib/components/lanes/LaneToolbar.svelte';
  import LaneWorkspace from '#lib/components/lanes/LaneWorkspace.svelte';
  import {
    filterLanes,
    type PipelineStageFilter,
    type PipelineTrafficFilter
  } from '#lib/components/lanes/lane-pipeline';
  import { laneKey, renameLane } from '#lib/components/lanes/lane-view';
  import type { LaneCategory, LanesResult } from '#lib/server/lanes';

  let { data } = $props();

  const { t } = getI18n();
  const POLL_MS = 5_000;
  const HIDDEN_POLL_MS = 60_000;
  const canMutate = $derived(allows(data.role, 'lanes.mutate'));

  let result = $state.raw<LanesResult | null>(null);
  let delayed = $state(false);
  let search = $state('');
  let stage = $state<PipelineStageFilter>('all');
  let traffic = $state<PipelineTrafficFilter>('all');
  const categories = new SvelteSet<LaneCategory>();
  const filtered = $derived(filterLanes(result?.lanes ?? [], { query: search, categories, stage, traffic }));

  $effect(() => {
    const signal = getAbortSignal();
    const pending = data.lanes;
    let stop: (() => void) | undefined;
    void pending.catch(() => ({ lanes: [], degraded: true, notice: '' })).then((snapshot) => {
      if (signal.aborted) return;
      accept(snapshot);
      stop = livePoll(() => pollLanes(signal), {
        firstDelayMs: POLL_MS,
        delayMs: () => POLL_MS,
        hiddenDelayMs: HIDDEN_POLL_MS,
        timeoutMs: Number.POSITIVE_INFINITY,
        refreshOnVisible: true
      });
    });
    return () => stop?.();
  });

  function accept(snapshot: LanesResult) {
    delayed = snapshot.degraded;
    if (!snapshot.degraded || snapshot.lanes.length || !result) result = snapshot;
  }

  function showUnavailable() {
    delayed = true;
    result ??= { lanes: [], degraded: true, notice: '' };
  }

  async function pollLanes(signal: AbortSignal): Promise<boolean> {
    if (document.hidden || signal.aborted) return false;
    try {
      const response = await fetch('/lanes/data', { signal });
      if (!response.ok) throw new Error(String(response.status));
      const snapshot: LanesResult = await response.json();
      if (!signal.aborted) accept(snapshot);
    } catch {
      if (!signal.aborted) showUnavailable();
    }
    return false;
  }

  function toggleCategory(category: LaneCategory | 'all') {
    if (category === 'all') {
      categories.clear();
      return;
    }
    if (!categories.delete(category)) categories.add(category);
  }

  function onRename(key: string, alias: string) {
    if (!result) return;
    const lanes = result.lanes.map((lane) => laneKey(lane) === key ? renameLane(lane, alias) : lane);
    result = { ...result, lanes };
  }

  function refresh() {
    void invalidate('admin:lanes').catch(showUnavailable);
  }
</script>

<section class="screen active">
  <PageHead description={t('admin.lanes.description')}>
    <bb-fit><span data-fit>{t('admin.lanes.titlePre')}<em>{t('admin.lanes.titleEm')}</em></span></bb-fit>
  </PageHead>

  <LaneToolbar
    {result}
    {delayed}
    {filtered}
    {categories}
    {toggleCategory}
    onRefresh={refresh}
    bind:search
    bind:stage
    bind:traffic
  />

  <LaneWorkspace
    {result}
    rows={filtered.rows}
    {canMutate}
    {onRename}
    onRefresh={refresh}
  />
</section>
