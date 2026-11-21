<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { untrack, onMount } from 'svelte';
  import { SvelteSet } from 'svelte/reactivity';
  import { enhance, type SubmitFunction } from '$app/forms';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import DeckLayout from '@bagel/ui/svelte/DeckLayout.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import InspectorSurface from '@bagel/ui/svelte/InspectorSurface.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import { createInspector } from '@bagel/ui/svelte/inspector';
  import { createDiscardGuard } from '@bagel/ui/svelte/discard-guard';
  import { livePoll } from '@bagel/ui/lib/live-poll';
  import { toast } from '@bagel/ui/svelte/toast';
  import { actionPayload, adminToastFailure } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { allows } from '#lib/access';
  import { applyStreamed } from '#lib/streamed-load';
  import type { LaneView, LanesResult } from '#lib/server/lanes';
  import LaneRow from '#lib/components/lanes/LaneRow.svelte';
  import LaneEditor from '#lib/components/lanes/LaneEditor.svelte';
  import {
    matchesPipeline,
    type PipelineStageFilter,
    type PipelineTrafficFilter
  } from '#lib/components/lanes/lane-pipeline';
  import {
    currentAlias,
    groupLanes,
    laneKey,
    normalizeAlias,
    type LaneDraft
  } from '#lib/components/lanes/lane-view';

  let { data } = $props();

  const { locale, t } = getI18n();
  const failed = adminToastFailure(toast);
  const canMutate = $derived(allows(data.role, 'lanes.mutate'));

  let result = $state.raw<LanesResult | null>(null);
  let live = $state(false);
  $effect(() => applyStreamed(data.lanes, (snapshot) => {
    if (result === null) result = snapshot;
  }));

  const POLL_MS = 5000;

  async function pollLanes(): Promise<boolean> {
    if (typeof document !== 'undefined' && document.hidden) return false;
    try {
      const res = await fetch('/lanes/data');
      if (!res.ok) {
        live = false;
        return false;
      }
      result = (await res.json()) as LanesResult;
      live = !result.degraded;
    } catch {
      live = false;
    }
    return false;
  }

  onMount(() =>
    livePoll(pollLanes, {
      firstDelayMs: POLL_MS,
      delayMs: () => POLL_MS,
      timeoutMs: Number.POSITIVE_INFINITY,
      refreshOnVisible: true
    })
  );

  const categoryChoices = [
    { value: 'all', label: t('admin.lanes.categoryAll') },
    { value: 'system', label: t('admin.lanes.categorySystem') },
    { value: 'projection', label: t('admin.lanes.categoryProjection') },
    { value: 'ephemeral', label: t('admin.lanes.categoryEphemeral') }
  ] as const;
  const pipelineChoices = [
    { value: 'all', label: t('admin.lanes.pipelineAll') },
    { value: 'twitch', label: t('admin.lanes.pipelineTwitch') },
    { value: 'ingress', label: t('admin.lanes.pipelineIngress') },
    { value: 'outgress', label: t('admin.lanes.pipelineOutgress') },
    { value: 'system', label: t('admin.lanes.pipelineSystem') }
  ] as const;
  const trafficChoices = [
    { value: 'all', label: t('admin.lanes.trafficAll') },
    { value: 'stream', label: t('admin.lanes.trafficStream') },
    { value: 'standard', label: t('admin.lanes.trafficStandard') },
    { value: 'premium', label: t('admin.lanes.trafficPremium') }
  ] as const;

  const categories = new SvelteSet<string>();
  let search = $state('');
  let pipelineStage = $state<PipelineStageFilter>('all');
  let trafficTier = $state<PipelineTrafficFilter>('all');

  const lanes = $derived(result?.lanes ?? []);
  const query = $derived(search.trim().toLowerCase());

  function matchesSearch(lane: LaneView): boolean {
    if (!query) return true;
    return [lane.display, lane.stream, lane.consumer, lane.subject]
      .some((field) => field.toLowerCase().includes(query));
  }

  function matchesCategory(lane: LaneView): boolean {
    return categories.size === 0 || categories.has(lane.category);
  }

  function toggleCategory(category: string) {
    if (category === 'all') {
      categories.clear();
      return;
    }
    if (!categories.delete(category)) categories.add(category);
  }

  type FilterChoice<Value> = { value: Value; label: string };
  const optionLayout = 'display: grid; grid-template-columns: minmax(0, 1fr) auto; overflow: clip;';

  function countedOptions<Value extends string>(
    options: readonly FilterChoice<Value>[],
    candidates: LaneView[],
    matches: (lane: LaneView, choice: Value) => boolean
  ) {
    return options.map(({ value, label }) => ({
      value,
      label,
      count: result === null ? '\u00a0' : candidates.filter((lane) => matches(lane, value)).length.toLocaleString(locale),
      attrs: { style: optionLayout }
    }));
  }

  const searched = $derived(lanes.filter(matchesSearch));
  const categoryCandidates = $derived(searched.filter((lane) => matchesPipeline(lane, pipelineStage, trafficTier)));
  const pipelineCandidates = $derived(searched.filter(matchesCategory));
  const categoryOptions = $derived(countedOptions(
    categoryChoices, categoryCandidates, (lane, value) => value === 'all' || value === lane.category
  ));
  const segmentFilters = $derived([
    {
      id: 'pipeline',
      label: t('admin.lanes.pipelineFilter'),
      options: countedOptions(pipelineChoices, pipelineCandidates, (lane, value) => matchesPipeline(lane, value, trafficTier)),
      value: pipelineStage,
      onValueChange: (value: string) => { pipelineStage = value as PipelineStageFilter; }
    },
    {
      id: 'traffic',
      label: t('admin.lanes.trafficFilter'),
      options: countedOptions(trafficChoices, pipelineCandidates, (lane, value) => matchesPipeline(lane, pipelineStage, value)),
      value: trafficTier,
      onValueChange: (value: string) => { trafficTier = value as PipelineTrafficFilter; }
    }
  ]);
  const rows = $derived(categoryCandidates.filter(matchesCategory));
  const groups = $derived(groupLanes(rows));

  const orphanCount = $derived(lanes.filter((l) => l.orphan).length);
  const totalPending = $derived(lanes.reduce((sum, l) => sum + l.pending, 0));

  const inspector = createInspector<LaneDraft>();
  let draft = $state<LaneDraft | null>(null);
  let busy = $state(false);

  $effect(() => {
    const snap = draft ? { ...draft } : null;
    if (snap) untrack(() => inspector.edit(snap));
  });

  const selected = $derived(lanes.find((l) => laneKey(l) === inspector.selectedId) ?? null);
  const canSave = $derived(inspector.dirty && !!draft && canMutate);

  const discard = createDiscardGuard(
    () => inspector.dirty,
    () => {
      inspector.reset();
      draft = null;
    }
  );

  function openLane(lane: LaneView) {
    if (inspector.selectedId === laneKey(lane)) {
      close();
      return;
    }
    discard.guard(() => {
      const seed: LaneDraft = { alias: currentAlias(lane) };
      inspector.open(laneKey(lane), seed);
      draft = { ...seed };
    });
  }

  function close() {
    discard.guard(() => {
      inspector.reset();
      draft = null;
    });
  }

  type LaneActionPayload = { ok?: boolean; notice?: string; error?: string };

  function renameInSnapshot(before: LanesResult | null, key: string | null, alias: string) {
    if (!before) return before;
    const next = normalizeAlias(alias);
    const lanes = before.lanes.map((lane) =>
      laneKey(lane) === key ? { ...lane, display: next || lane.consumer } : lane
    );
    return { ...before, lanes };
  }

  const aliasSubmit: SubmitFunction = () => {
    const begun = inspector.beginSave();
    const before = result;
    const optimistic = begun ? renameInSnapshot(before, inspector.selectedId, begun.snapshot.alias) : before;
    result = optimistic;
    busy = true;
    return async ({ result: r }) => {
      busy = false;
      const p = actionPayload<LaneActionPayload>(r);
      const ok = p?.ok === true;
      if (begun) inspector.resolved(begun.requestId, { type: ok ? 'success' : 'error' });
      if (ok) {
        toast('success', p!.notice ?? t('admin.lanes.renamed'));
        return;
      }
      if (result === optimistic) result = before;
      failed(p, t('admin.lanes.renameFailed'));
    };
  };

  let confirmDurable = $state(false);
  let confirmDelete = $state(false);
  let durableForm = $state<HTMLFormElement | null>(null);
  let deleteForm = $state<HTMLFormElement | null>(null);

  function laneAction(after: () => void, fallback: string): SubmitFunction {
    return () => {
      busy = true;
      return async ({ result: r }) => {
        busy = false;
        after();
        const p = actionPayload<LaneActionPayload>(r);
        if (p?.ok) {
          toast('success', p.notice ?? t('admin.lanes.done'));
          pollLanes();
          return;
        }
        failed(p, fallback);
      };
    };
  }

  const durableSubmit = laneAction(
    () => (confirmDurable = false),
    t('admin.lanes.durableFailed')
  );
  const deleteSubmit = laneAction(() => {
    confirmDelete = false;
    inspector.reset();
    draft = null;
  }, t('admin.lanes.deleteFailed'));
</script>

<section class="screen active">
  <PageHead eyebrow={t('admin.lanes.eyebrow')} description={t('admin.lanes.description')}>
    {t('admin.lanes.titlePre')}<em>{t('admin.lanes.titleEm')}</em>
  </PageHead>

  {#if result?.degraded}
    <AlertBanner>{result.notice}</AlertBanner>
  {/if}

  <div class="lane-toolbar">
    <PageToolbar style="display: block;">
      {#snippet leading()}
        <Stack gap={3}>
          <div class="toolbar-top">
            <div class="toolbar-summary" aria-busy={result === null}>
              {#if result}
                <Text as="div" size="xs" tone="muted" mono>
                  <bb-fit>{t('admin.lanes.stats', {
                    lanes: lanes.length.toLocaleString(locale),
                    orphans: orphanCount.toLocaleString(locale),
                    pending: totalPending.toLocaleString(locale)
                  })}</bb-fit>
                </Text>
              {:else}
                <Skeleton variant="pill" width="min(100%, 220px)" height="16px" />
              {/if}
              <span class="live-indicator" class:live aria-hidden={!live}>
                <Tag tone="live" bare style="display: flex;"><bb-fit>{t('admin.lanes.live')}</bb-fit></Tag>
              </span>
            </div>
            <SearchInput
              fill
              bind:value={search}
              placeholder={t('admin.lanes.searchPlaceholder')}
              aria-label={t('admin.lanes.searchPlaceholder')}
            />
          </div>
          <div class="toolbar-filters">
            <Stack gap={3}>
              {#each segmentFilters as filter (filter.id)}
                <Stack gap={1}>
                  <Label mono as="span">{filter.label}</Label>
                  <SegmentedControl
                    even
                    class="bb-tabs--plain"
                    label={filter.label}
                    options={filter.options}
                    value={filter.value}
                    onValueChange={filter.onValueChange}
                    style="grid-auto-columns: minmax(5.5rem, 1fr); overflow-x: auto; scrollbar-width: thin;"
                  />
                </Stack>
              {/each}
              <Stack gap={1}>
                <Label mono as="span" id="lane-category-filter">{t('admin.lanes.categoryFilter')}</Label>
                <Grid min="8rem" gap={2} class="lane-categories" role="group" aria-labelledby="lane-category-filter">
                  {#each categoryOptions as option (option.value)}
                    <Chip
                      class="bb-chip--cell"
                      style={optionLayout}
                      pressed={option.value === 'all' ? categories.size === 0 : categories.has(option.value)}
                      onclick={() => toggleCategory(option.value)}
                    >
                      <bb-fit>{option.label}</bb-fit>
                      <Text as="span" size="xs" tone="muted" mono>{option.count}</Text>
                    </Chip>
                  {/each}
                </Grid>
              </Stack>
            </Stack>
          </div>
        </Stack>
      {/snippet}
    </PageToolbar>
  </div>

  <Stack gap={4}>
    <DeckLayout inspecting={inspector.isOpen} width="380px">
      <DeckList>
        {#if result === null}
          <SkeletonStack rows={5} height="56px" />
        {:else if rows.length}
          <Stack gap={5}>
            {#each groups as group (group.stream)}
              <section>
                <header class="stream-heading">
                  <Cluster justify="between" align="baseline" gap={2}>
                    <Heading level={2} variant="eyebrow">{group.stream}</Heading>
                    <Text as="span" size="xs" tone="muted">{t('admin.lanes.groupSummary', {
                      lanes: String(group.lanes.length), pending: group.pending.toLocaleString()
                    })}</Text>
                  </Cluster>
                </header>
                <ul class="bb-list" aria-label={`${t('admin.lanes.listLabel')} · ${group.stream}`}>
                  {#each group.lanes as lane (laneKey(lane))}
                    <li>
                      <LaneRow
                        {lane}
                        selected={inspector.selectedId === laneKey(lane)}
                        controls="lane-inspector"
                        onselect={() => openLane(lane)}
                      />
                    </li>
                  {/each}
                </ul>
              </section>
            {/each}
          </Stack>
        {:else if lanes.length}
          <EmptyState title={t('admin.lanes.emptyMatch')} />
        {:else}
          <EmptyState
            title={t('admin.lanes.empty')}
            body={result.degraded ? t('admin.lanes.emptyDegraded') : t('admin.lanes.emptyBody')}
          />
        {/if}
      </DeckList>

      {#if inspector.isOpen && draft && selected}
        <InspectorSurface
          open
          title={selected.display}
          controls="lane-inspector"
          closeLabel={t('admin.close')}
          onClose={close}
        >
          {#key inspector.selectedId}
            <LaneEditor
              bind:draft={
                () => draft!,
                (v) => (draft = v)
              }
              lane={selected}
              {canMutate}
              status={inspector.status}
              dirty={inspector.dirty}
              {canSave}
              {busy}
              onCancel={close}
              onSubmit={aliasSubmit}
              onDurable={() => (confirmDurable = true)}
              onDelete={() => (confirmDelete = true)}
            />
          {/key}
        </InspectorSurface>
      {/if}
    </DeckLayout>
  </Stack>
</section>

<ConfirmDialog
  open={confirmDurable}
  title={t('admin.lanes.confirmDurableTitle')}
  body={selected
    ? t('admin.lanes.confirmDurableBody', {
        name: selected.display,
        stream: selected.stream
      })
    : undefined}
  confirmLabel={t('admin.lanes.makePermanent')}
  cancelLabel={t('common.cancel')}
  {busy}
  onCancel={() => (confirmDurable = false)}
  onConfirm={() => durableForm?.requestSubmit()}
/>
<form method="POST" action="?/durable" use:enhance={durableSubmit} bind:this={durableForm} hidden>
  <input type="hidden" name="stream" value={selected?.stream ?? ''} />
  <input type="hidden" name="consumer" value={selected?.consumer ?? ''} />
</form>

<ConfirmDialog
  open={confirmDelete}
  title={t('admin.lanes.confirmDeleteTitle')}
  body={selected
    ? t('admin.lanes.confirmDeleteBody', {
        consumer: selected.consumer,
        stream: selected.stream
      })
    : undefined}
  confirmLabel={t('common.delete')}
  cancelLabel={t('common.cancel')}
  {busy}
  onCancel={() => (confirmDelete = false)}
  onConfirm={() => deleteForm?.requestSubmit()}
  tone="danger"
/>
<form method="POST" action="?/delete" use:enhance={deleteSubmit} bind:this={deleteForm} hidden>
  <input type="hidden" name="stream" value={selected?.stream ?? ''} />
  <input type="hidden" name="consumer" value={selected?.consumer ?? ''} />
</form>

<ConfirmDialog
  open={discard.open}
  title={t('admin.unsaved')}
  confirmLabel={t('common.done')}
  cancelLabel={t('common.cancel')}
  onConfirm={discard.confirm}
  onCancel={discard.cancel}
/>

<style>
  .stream-heading {
    padding: var(--bb-space-3) var(--bb-space-4) var(--bb-space-2);
    overflow-wrap: anywhere;
  }

  .lane-toolbar {
    container-type: inline-size;
  }

  .toolbar-top {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 15rem);
    align-items: center;
    gap: var(--bb-space-3);
  }

  .toolbar-summary {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 5.5rem;
    align-items: center;
    gap: var(--bb-space-2);
    min-block-size: 22px;
  }

  .live-indicator {
    max-inline-size: 5.5rem;
    opacity: 0;
    transition: opacity var(--bb-dur-base) var(--bb-ease-out-expo);
  }

  .live-indicator.live {
    opacity: 1;
  }

  .toolbar-filters {
    max-inline-size: 42rem;
  }

  @container (max-width: 42rem) {
    .toolbar-top {
      grid-template-columns: minmax(0, 1fr);
    }
  }

  @container (max-width: 34rem) {
    .toolbar-filters :global(.lane-categories) {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }

  @container (max-width: 20rem) {
    .toolbar-summary {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
