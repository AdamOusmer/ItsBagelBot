<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { untrack, onMount } from 'svelte';
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import DeckLayout from '@bagel/ui/svelte/DeckLayout.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
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
  import { allows } from '$lib/access';
  import type { LaneView, LanesResult } from '$lib/server/lanes';
  import LaneRow from '$lib/components/lanes/LaneRow.svelte';
  import LaneEditor from '$lib/components/lanes/LaneEditor.svelte';
  import { matchesPipeline } from '$lib/components/lanes/lane-pipeline';
  import {
    currentAlias,
    groupLanes,
    laneKey,
    normalizeAlias,
    type LaneDraft
  } from '$lib/components/lanes/lane-view';

  let { data } = $props();

  const { t } = getI18n();
  const failed = adminToastFailure(toast);
  const canMutate = $derived(allows(data.role, 'lanes.mutate'));

  let result = $state<LanesResult | null>(null);
  let live = $state(false);
  $effect(() => {
    let alive = true;
    data.lanes.then((r: LanesResult) => {
      if (alive && result === null) result = r;
    });
    return () => {
      alive = false;
    };
  });

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

  const CATEGORIES = ['all', 'system', 'projection', 'ephemeral'] as const;
  let category = $state<string>('all');
  let search = $state('');
  const PIPELINE_STAGES = ['all', 'twitch', 'ingress', 'outgress', 'system'] as const;
  const TRAFFIC_TIERS = ['all', 'stream', 'standard', 'premium'] as const;
  let pipelineStage = $state<(typeof PIPELINE_STAGES)[number]>('all');
  let trafficTier = $state<(typeof TRAFFIC_TIERS)[number]>('all');
  const pipelineOptions = $derived([
    t('admin.lanes.pipelineAll'), t('admin.lanes.pipelineTwitch'),
    t('admin.lanes.pipelineIngress'), t('admin.lanes.pipelineOutgress'),
    t('admin.lanes.pipelineSystem')
  ]);
  const trafficOptions = $derived([
    t('admin.lanes.trafficAll'), t('admin.lanes.trafficStream'),
    t('admin.lanes.trafficStandard'), t('admin.lanes.trafficPremium')
  ]);

  function selectPipeline(label: string) {
    pipelineStage = PIPELINE_STAGES[pipelineOptions.indexOf(label)] ?? 'all';
    category = 'all';
  }

  function selectTraffic(label: string) {
    trafficTier = TRAFFIC_TIERS[trafficOptions.indexOf(label)] ?? 'all';
    category = 'all';
  }

  const lanes = $derived(result?.lanes ?? []);

  function matchesSearch(lane: LaneView, q: string): boolean {
    if (!q) return true;
    return (
      lane.display.toLowerCase().includes(q) ||
      lane.stream.toLowerCase().includes(q) ||
      lane.consumer.toLowerCase().includes(q) ||
      lane.subject.toLowerCase().includes(q)
    );
  }

  const rows = $derived.by(() => {
    const q = search.trim().toLowerCase();
    return lanes.filter(
      (l) => (category === 'all' || l.category === category) &&
        matchesPipeline(l, pipelineStage, trafficTier) && matchesSearch(l, q)
    );
  });

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

  const aliasSubmit: SubmitFunction = () => {
    const begun = inspector.beginSave();
    const key = inspector.selectedId;
    const before = result ? result.lanes.map((l) => ({ ...l })) : null;
    busy = true;
    if (result && begun) {
      const next = normalizeAlias(begun.snapshot.alias);
      result.lanes = result.lanes.map((l) =>
        laneKey(l) === key ? { ...l, display: next || l.consumer } : l
      );
    }
    return async ({ result: r }) => {
      busy = false;
      const p = actionPayload<LaneActionPayload>(r);
      const ok = p?.ok === true;
      if (begun) inspector.resolved(begun.requestId, { type: ok ? 'success' : 'error' });
      if (ok) {
        toast('success', p!.notice ?? t('admin.lanes.renamed'));
        return;
      }
      if (result && before) result.lanes = before;
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

  <PageToolbar>
    {#snippet leading()}
      {#if result}
        <Cluster gap={2}>
          <Text as="span" size="xs" tone="muted" mono>
            {t('admin.lanes.stats', {
              lanes: String(lanes.length),
              orphans: String(orphanCount),
              pending: totalPending.toLocaleString()
            })}
          </Text>
          {#if live}<Tag tone="live">{t('admin.lanes.live')}</Tag>{/if}
        </Cluster>
      {:else}
        <Skeleton variant="pill" width="220px" />
      {/if}
    {/snippet}
    {#snippet trailing()}
      <div class="toolbar-search">
        <SearchInput fill bind:value={search} placeholder={t('admin.lanes.searchPlaceholder')} />
      </div>
    {/snippet}
  </PageToolbar>

  <Stack gap={4}>
    <Stack gap={3}>
      <Stack gap={2} align="start">
        <Label mono as="span">{t('admin.lanes.pipelineFilter')}</Label>
        <SegmentedControl
          options={pipelineOptions}
          label={t('admin.lanes.pipelineFilter')}
          bind:value={() => pipelineOptions[PIPELINE_STAGES.indexOf(pipelineStage)], selectPipeline}
        />
      </Stack>
      <Stack gap={2} align="start">
        <Label mono as="span">{t('admin.lanes.trafficFilter')}</Label>
        <SegmentedControl
          options={trafficOptions}
          label={t('admin.lanes.trafficFilter')}
          bind:value={() => trafficOptions[TRAFFIC_TIERS.indexOf(trafficTier)], selectTraffic}
        />
      </Stack>
      {#if pipelineStage === 'all' && trafficTier === 'all'}
        <Stack gap={2} align="start">
          <Label mono as="span">{t('admin.lanes.categoryFilter')}</Label>
          <SegmentedControl
            options={CATEGORIES}
            label={t('admin.lanes.categoryFilter')}
            bind:value={category}
          />
        </Stack>
      {/if}
    </Stack>

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

  .toolbar-search {
    width: 240px;
  }

  @media (max-width: 680px) {
    .toolbar-search {
      width: 100%;
    }
  }
</style>
