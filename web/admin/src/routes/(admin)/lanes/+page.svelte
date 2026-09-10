<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // JetStream consumers, on the shared deck + inspector.
  //
  // The page was a seven-column CSS grid with a hand-written header row, an
  // inline rename <input> that appeared INSIDE the row it renamed, and three
  // icon buttons per row. Rename, make-permanent and delete all moved into the
  // inspector; the grid became a DeckList.
  //
  // The action names (`alias`, `durable`, `delete`) are the server's LaneOp
  // table's and are not renamed -- the audit trail keys off them.
  import { untrack, onMount } from 'svelte';
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import DeckList from '@bagel/kit/components/DeckList.svelte';
  import InspectorSurface from '@bagel/kit/components/InspectorSurface.svelte';
  import AlertBanner from '@bagel/kit/components/AlertBanner.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import ConfirmDialog from '@bagel/kit/components/ConfirmDialog.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import { createInspector } from '@bagel/kit/inspector';
  import { createDiscardGuard } from '@bagel/kit/discard-guard';
  import { livePoll } from '@bagel/kit/live-poll';
  import { toast } from '@bagel/kit/toast';
  import { actionPayload, adminToastFailure } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { allows } from '$lib/access';
  import type { LaneView, LanesResult } from '$lib/server/lanes';
  import LaneRow from '$lib/components/lanes/LaneRow.svelte';
  import LaneEditor from '$lib/components/lanes/LaneEditor.svelte';
  import {
    currentAlias,
    laneKey,
    normalizeAlias,
    type LaneDraft
  } from '$lib/components/lanes/lane-view';

  let { data } = $props();

  const { t } = getI18n();
  const failed = adminToastFailure(toast);
  const canMutate = $derived(allows(data.role, 'lanes.mutate'));

  // ── Streamed lanes -> local state ──────────────────────────────────────────
  let result = $state<LanesResult | null>(null);
  let live = $state(false);
  $effect(() => {
    let alive = true;
    data.lanes.then((r: LanesResult) => {
      // The poll may already have delivered something fresher than SSR.
      if (alive && result === null) result = r;
    });
    return () => {
      alive = false;
    };
  });

  // ── Live poll ──────────────────────────────────────────────────────────────
  // The sampler keeps a warm snapshot behind /lanes/data, so this is a flat 5s
  // like the loop it replaces. livePoll owns the generation counter that makes a
  // teardown mid-fetch safe; the tick never reports settled, because lane depth
  // never "arrives", so the deadline is Infinity.
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
      live = true;
    } catch {
      live = false;
    }
    return false;
  }

  onMount(() => {
    const stop = livePoll(pollLanes, {
      firstDelayMs: POLL_MS,
      delayMs: () => POLL_MS,
      timeoutMs: Number.POSITIVE_INFINITY
    });
    const onVis = () => {
      if (!document.hidden) pollLanes();
    };
    document.addEventListener('visibilitychange', onVis);
    return () => {
      stop();
      document.removeEventListener('visibilitychange', onVis);
    };
  });

  // ── Filters ────────────────────────────────────────────────────────────────
  const CATEGORIES = ['all', 'system', 'projection', 'ephemeral'] as const;
  let category = $state<string>('all');
  let search = $state('');

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
      (l) => (category === 'all' || l.category === category) && matchesSearch(l, q)
    );
  });

  const orphanCount = $derived(lanes.filter((l) => l.orphan).length);
  const totalPending = $derived(lanes.reduce((sum, l) => sum + l.pending, 0));

  // ── Inspector ──────────────────────────────────────────────────────────────
  const inspector = createInspector<LaneDraft>();
  let draft = $state<LaneDraft | null>(null);
  let busy = $state(false);

  // Push editor changes into the machine for dirty tracking. The spread reads
  // the field so the effect re-runs on any mutation; the edit itself is
  // untracked because it both reads and writes the machine's state, which would
  // otherwise make the effect depend on state it also mutates (an unsafe cycle).
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

  // ── Mutations ──────────────────────────────────────────────────────────────
  // All three answer LaneMutationResult (`{ ok, notice }`) or fail(502, {notice}).
  type LaneActionPayload = { ok?: boolean; notice?: string; error?: string };

  // Optimistic alias: the row shows the new name immediately, and the old one
  // comes back with the real error if the KV write fails.
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
        toast('ok', p!.notice ?? t('admin.lanes.renamed'));
        return;
      }
      if (result && before) result.lanes = before;
      failed(p, t('admin.lanes.renameFailed'));
    };
  };

  // ── Make permanent / delete (confirmed, non-optimistic) ────────────────────
  // Neither outcome is locally derivable: `durable` creates a NEW consumer the
  // sampler has yet to see, and `delete` is refused server-side when anything is
  // still bound. Both re-poll instead of guessing.
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
          toast('ok', p.notice ?? t('admin.lanes.done'));
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
    {#snippet lead()}
      {#if result}
        <span class="stats">
          {t('admin.lanes.stats', {
            lanes: String(lanes.length),
            orphans: String(orphanCount),
            pending: totalPending.toLocaleString()
          })}
          {#if live}<span class="live">{t('admin.lanes.live')}</span>{/if}
        </span>
      {:else}
        <Skeleton variant="pill" width="220px" />
      {/if}
    {/snippet}
    {#snippet trail()}
      <div class="toolbar-search">
        <SearchInput bind:value={search} placeholder={t('admin.lanes.searchPlaceholder')} />
      </div>
    {/snippet}
  </PageToolbar>

  <div class="filters">
    <SegmentedControl
      options={CATEGORIES}
      label={t('admin.lanes.categoryFilter')}
      bind:value={category}
    />
  </div>

  <div class="deck" class:inspecting={inspector.isOpen}>
    <DeckList>
      {#if result === null}
        <SkeletonStack rows={5} height="56px" />
      {:else if rows.length}
        <ul class="bb-list" aria-label={t('admin.lanes.listLabel')}>
          {#each rows as lane (laneKey(lane))}
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
        <!-- Keyed on the selection so switching rows mounts a FRESH editor: the
             alias field binds to the draft snapshot taken at open, so one reused
             instance would freeze it to the first lane opened. -->
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
  </div>
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
  danger
  {busy}
  onCancel={() => (confirmDelete = false)}
  onConfirm={() => deleteForm?.requestSubmit()}
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
  .stats {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-muted);
  }
  .live {
    color: var(--bb-green-glow);
    letter-spacing: 0.08em;
    text-transform: uppercase;
    font-size: 10px;
    margin-left: 8px;
  }

  .toolbar-search {
    width: 240px;
  }
  .toolbar-search :global(.bb-input) {
    width: 100%;
  }

  .filters {
    margin: 0 0 14px;
  }

  .deck {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 16px;
    align-items: start;
  }
  @media (min-width: 1080px) {
    .deck.inspecting {
      grid-template-columns: minmax(0, 1fr) 380px;
    }
  }

  @media (max-width: 680px) {
    .toolbar-search {
      width: 100%;
    }
  }
</style>
