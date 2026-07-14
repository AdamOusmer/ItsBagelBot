<script lang="ts">
  import { enhance } from '$app/forms';
  import { onMount } from 'svelte';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Icon,
    PageHead,
    PageToolbar,
    SegmentedControl,
    SearchInput,
    AlertBanner,
    DeckList,
    EmptyState,
    ConfirmDialog,
    Skeleton,
    toast
  } from '@bagel/shared';
  import type { LaneView, LanesResult } from '$lib/server/lanes';

  let { data } = $props();

  // ── Streamed lanes -> local state + live poll ──────────────────────────────
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

  async function poll() {
    if (typeof document !== 'undefined' && document.hidden) return;
    try {
      const res = await fetch('/lanes/data');
      if (!res.ok) {
        live = false;
        return;
      }
      result = (await res.json()) as LanesResult;
      live = true;
    } catch {
      live = false;
    }
  }

  onMount(() => {
    const timer = setInterval(poll, 5000);
    const onVis = () => {
      if (!document.hidden) poll();
    };
    document.addEventListener('visibilitychange', onVis);
    return () => {
      clearInterval(timer);
      document.removeEventListener('visibilitychange', onVis);
    };
  });

  // ── Filters ────────────────────────────────────────────────────────────────
  const CATEGORIES = ['all', 'system', 'projection', 'ephemeral'] as const;
  let category = $state<string>('all');
  let search = $state('');

  const lanes = $derived(result?.lanes ?? []);
  const rows = $derived(
    lanes.filter((l) => {
      if (category !== 'all' && l.category !== category) return false;
      const q = search.trim().toLowerCase();
      if (!q) return true;
      return (
        l.display.toLowerCase().includes(q) ||
        l.stream.toLowerCase().includes(q) ||
        l.consumer.toLowerCase().includes(q) ||
        l.subject.toLowerCase().includes(q)
      );
    })
  );

  const orphanCount = $derived(lanes.filter((l) => l.orphan).length);
  const totalPending = $derived(lanes.reduce((sum, l) => sum + l.pending, 0));

  function laneKey(l: LaneView): string {
    return `${l.stream} ${l.consumer}`;
  }

  // ── Rename (optimistic alias) ──────────────────────────────────────────────
  let renaming = $state<string | null>(null);
  let renameValue = $state('');
  let renameForm = $state<HTMLFormElement | null>(null);
  let renameTarget = $state<LaneView | null>(null);
  let pendingRenameRollback: LaneView[] | null = null;

  function openRename(l: LaneView) {
    renaming = laneKey(l);
    renameTarget = l;
    renameValue = l.display === l.consumer || l.display === 'ephemeral' ? '' : l.display;
  }

  function submitRename() {
    if (!renameTarget || !result) return;
    const key = laneKey(renameTarget);
    const before = result.lanes.map((l) => ({ ...l }));
    const next = renameValue.trim().slice(0, 48);
    // Optimistic: the row shows the new name immediately; the old name comes
    // back (with the real error) if the KV write fails.
    result.lanes = result.lanes.map((l) =>
      laneKey(l) === key ? { ...l, display: next || l.consumer } : l
    );
    pendingRenameRollback = before;
    queueMicrotask(() => renameForm?.requestSubmit());
    renaming = null;
  }

  type LaneActionPayload = { ok?: boolean; notice?: string; error?: string };
  function payloadOf(r: unknown): LaneActionPayload | undefined {
    const res = r as { type: string; data?: LaneActionPayload };
    return res.type === 'success' || res.type === 'failure' ? res.data : undefined;
  }

  const renameSubmit: SubmitFunction = () => {
    return async ({ result: r }) => {
      const p = payloadOf(r);
      if (p?.ok) {
        toast('ok', p.notice ?? 'renamed');
        pendingRenameRollback = null;
        poll();
        return;
      }
      if (result && pendingRenameRollback) result.lanes = pendingRenameRollback;
      pendingRenameRollback = null;
      toast('err', p?.error ?? p?.notice ?? 'rename failed');
    };
  };

  // ── Make permanent / delete (confirmed, non-optimistic) ────────────────────
  let confirmDurable = $state<LaneView | null>(null);
  let confirmDelete = $state<LaneView | null>(null);
  let durableForm = $state<HTMLFormElement | null>(null);
  let deleteForm = $state<HTMLFormElement | null>(null);
  let busy = $state(false);

  function laneAction(close: () => void, failMsg: string): SubmitFunction {
    return () => {
      busy = true;
      return async ({ result: r }) => {
        busy = false;
        close();
        const p = payloadOf(r);
        if (p?.ok) {
          toast('ok', p.notice ?? 'done');
          poll();
          return;
        }
        toast('err', p?.error ?? p?.notice ?? failMsg);
      };
    };
  }
  const durableSubmit = laneAction(() => (confirmDurable = null), 'make-permanent failed');
  const deleteSubmit = laneAction(() => (confirmDelete = null), 'delete failed');
</script>

<section class="screen active">
  <div class="page-head">
    <span class="eyebrow">
      JetStream consumers
      {#if live}<span class="live-chip"><span class="live-dot"></span> live</span>{/if}
    </span>
    <h1>Message <em>lanes</em></h1>
    {#if result}
      <p>
        {lanes.length} lanes · {orphanCount} orphan{orphanCount === 1 ? '' : 's'} ·
        {totalPending.toLocaleString()} pending fleet-wide
      </p>
    {:else}
      <p>Collecting stream and consumer state…</p>
    {/if}
  </div>

  {#if result?.degraded}
    <AlertBanner>{result.notice}</AlertBanner>
  {/if}

  <PageToolbar>
    {#snippet lead()}
      <SegmentedControl options={CATEGORIES} bind:value={category} label="Lane category" />
    {/snippet}
    {#snippet trail()}
      <div class="toolbar-search">
        <SearchInput bind:value={search} placeholder="Filter lanes…" />
      </div>
    {/snippet}
  </PageToolbar>

  <DeckList>
    {#if result === null}
      <div class="row-skeletons">
        {#each [0, 1, 2, 3, 4] as i (i)}<Skeleton variant="block" height="56px" />{/each}
      </div>
    {:else if rows.length}
      <div class="lane-head" aria-hidden="true">
        <span>lane</span><span>subject</span><span class="num">pending</span>
        <span class="num">in-flight</span><span class="num">rate</span><span class="num">redeliv</span><span></span>
      </div>
      <ul class="list" aria-label="Lanes">
        {#each rows as l (laneKey(l))}
          <li class="lane-row" class:orphan={l.orphan}>
            <div class="lane-id">
              <span class="ldot {l.orphan ? 'err' : l.ephemeral ? 'warn' : ''}"></span>
              <div class="lane-names">
                {#if renaming === laneKey(l)}
                  <form
                    class="rename-inline"
                    onsubmit={(e) => {
                      e.preventDefault();
                      submitRename();
                    }}
                  >
                    <!-- svelte-ignore a11y_autofocus -->
                    <input
                      class="text-input"
                      type="text"
                      maxlength="48"
                      placeholder={l.consumer}
                      bind:value={renameValue}
                      autofocus
                      onkeydown={(e) => {
                        if (e.key === 'Escape') renaming = null;
                      }}
                    />
                    <button class="mini-act" type="submit" aria-label="Save name"><Icon name="check" size={13} /></button>
                    <button class="mini-act" type="button" aria-label="Cancel" onclick={() => (renaming = null)}>
                      <Icon name="x" size={13} />
                    </button>
                  </form>
                {:else}
                  <span class="lname">
                    {l.display}
                    {#if l.ephemeral}<span class="tag warn">ephemeral</span>{/if}
                    {#if l.orphan}<span class="tag err">orphan</span>{/if}
                  </span>
                  <span class="lsub">{l.stream} · {l.consumer}</span>
                {/if}
              </div>
            </div>
            <span class="lsubject" title={l.subject}>{l.subject || '—'}</span>
            <span class="num {l.pending > 0 ? 'hot' : ''}">{l.pending.toLocaleString()}</span>
            <span class="num">{l.inFlight}</span>
            <span class="num">{l.rate}</span>
            <span class="num {l.redelivered > 0 ? 'warn-num' : ''}">{l.redelivered}</span>
            <span class="lane-actions">
              <button class="mini-act" type="button" title="Rename" aria-label="Rename lane" onclick={() => openRename(l)}>
                <Icon name="edit" size={13} />
              </button>
              {#if l.ephemeral && !l.orphan}
                <button
                  class="mini-act"
                  type="button"
                  title="Make permanent"
                  aria-label="Make lane permanent"
                  onclick={() => (confirmDurable = l)}
                >
                  <Icon name="lock" size={13} />
                </button>
              {/if}
              {#if l.orphan}
                <button
                  class="mini-act danger"
                  type="button"
                  title="Delete orphan"
                  aria-label="Delete orphan lane"
                  onclick={() => (confirmDelete = l)}
                >
                  <Icon name="trash" size={13} />
                </button>
              {/if}
            </span>
          </li>
        {/each}
      </ul>
    {:else if lanes.length === 0}
      <EmptyState
        icon="lanes"
        title="No lanes visible"
        body={result.degraded
          ? 'The JetStream API is unreachable; this list is not the fleet.'
          : 'No streams or consumers were returned.'}
      />
    {:else}
      <EmptyState icon="search" title="No lanes match" />
    {/if}
  </DeckList>
</section>

<!-- Hidden rename form: the inline editor applies optimistically, this carries
     the write; failure rolls the row back. -->
<form method="POST" action="?/alias" use:enhance={renameSubmit} bind:this={renameForm} hidden>
  <input type="hidden" name="stream" value={renameTarget?.stream ?? ''} />
  <input type="hidden" name="consumer" value={renameTarget?.consumer ?? ''} />
  <input type="hidden" name="alias" value={renameValue.trim().slice(0, 48)} />
</form>

<ConfirmDialog
  open={confirmDurable !== null}
  title="Make lane permanent"
  body={confirmDurable
    ? `Creates a durable copy of ${confirmDurable.display} (${confirmDurable.stream}) that survives consumer restarts.`
    : undefined}
  confirmLabel="Make permanent"
  cancelLabel="Cancel"
  busy={busy}
  onCancel={() => (confirmDurable = null)}
  onConfirm={() => durableForm?.requestSubmit()}
/>
<form method="POST" action="?/durable" use:enhance={durableSubmit} bind:this={durableForm} hidden>
  <input type="hidden" name="stream" value={confirmDurable?.stream ?? ''} />
  <input type="hidden" name="consumer" value={confirmDurable?.consumer ?? ''} />
</form>

<ConfirmDialog
  open={confirmDelete !== null}
  title="Delete orphan lane"
  body={confirmDelete
    ? `Deletes ${confirmDelete.consumer} from ${confirmDelete.stream}. The server refuses if a consumer is still bound.`
    : undefined}
  confirmLabel="Delete"
  cancelLabel="Cancel"
  danger
  busy={busy}
  onCancel={() => (confirmDelete = null)}
  onConfirm={() => deleteForm?.requestSubmit()}
/>
<form method="POST" action="?/delete" use:enhance={deleteSubmit} bind:this={deleteForm} hidden>
  <input type="hidden" name="stream" value={confirmDelete?.stream ?? ''} />
  <input type="hidden" name="consumer" value={confirmDelete?.consumer ?? ''} />
</form>

<style>
  .live-chip {
    display: inline-flex; align-items: center; gap: 5px; margin-left: 8px;
    color: var(--bb-green-glow); letter-spacing: 0.08em;
  }
  .live-dot {
    width: 6px; height: 6px; border-radius: 50%;
    background: var(--bb-green-glow); box-shadow: 0 0 8px var(--bb-green-glow);
    animation: live-pulse 2.4s ease-in-out infinite;
  }
  @keyframes live-pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }

  .toolbar-search { width: 240px; }
  .toolbar-search :global(.search) { width: 100%; }
  .row-skeletons { display: flex; flex-direction: column; gap: 8px; padding: 12px; }

  .lane-head, .lane-row {
    display: grid;
    grid-template-columns: minmax(180px, 1.6fr) minmax(0, 1.4fr) 74px 84px 90px 64px 96px;
    align-items: center;
    gap: 12px;
  }
  .lane-head {
    padding: 10px 14px 8px;
    border-bottom: 1px solid var(--rule-strong);
    font-family: var(--bb-font-mono); font-size: 10px; letter-spacing: 0.12em;
    text-transform: uppercase; color: var(--bb-muted);
  }
  .lane-head .num { text-align: right; }

  .list { list-style: none; margin: 0; padding: 0; }
  .lane-row { padding: 12px 14px; border-bottom: 1px solid var(--rule); }
  .lane-row:last-child { border-bottom: none; }
  .lane-row.orphan { background: rgba(176, 90, 70, 0.03); }

  .lane-id { display: flex; align-items: center; gap: 10px; min-width: 0; }
  .ldot { width: 8px; height: 8px; border-radius: 50%; background: var(--bb-green-glow); box-shadow: 0 0 8px var(--bb-green-glow); flex: none; }
  .ldot.warn { background: var(--bb-tan); box-shadow: 0 0 8px var(--bb-tan); }
  .ldot.err { background: #cf8a78; box-shadow: 0 0 8px rgba(176, 90, 70, 0.6); }

  .lane-names { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .lname {
    font-family: var(--bb-font-body); font-weight: 600; font-size: 13.5px; color: var(--bb-white);
    display: inline-flex; align-items: center; gap: 7px; min-width: 0;
  }
  .lsub {
    font-family: var(--bb-font-mono); font-size: 10.5px; color: var(--bb-muted);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .lsubject {
    font-family: var(--bb-font-mono); font-size: 11.5px; color: var(--bb-tan-light);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }

  .tag {
    font-family: var(--bb-font-mono); font-size: 9.5px; letter-spacing: 0.08em; text-transform: uppercase;
    padding: 1px 7px; border-radius: var(--bb-radius-pill); border: 1px solid transparent; flex: none;
  }
  .tag.warn { color: var(--bb-tan-light); background: rgba(201, 168, 124, 0.1); border-color: rgba(201, 168, 124, 0.28); }
  .tag.err { color: #cf8a78; background: rgba(176, 90, 70, 0.1); border-color: rgba(176, 90, 70, 0.3); }

  .num { font-family: var(--bb-font-mono); font-size: 12px; color: var(--bb-muted); text-align: right; white-space: nowrap; }
  .num.hot { color: var(--bb-tan-light); }
  .num.warn-num { color: #cf8a78; }

  .lane-actions { display: flex; gap: 4px; justify-content: flex-end; }
  .mini-act {
    width: 26px; height: 26px; border-radius: 7px;
    display: inline-flex; align-items: center; justify-content: center;
    background: none; border: 1px solid transparent; color: var(--bb-muted); cursor: pointer;
  }
  .mini-act :global(svg) { stroke: currentColor; fill: none; stroke-width: 1.7; }
  .mini-act:hover { color: var(--bb-white); background: rgba(255, 255, 255, 0.05); }
  .mini-act.danger:hover { color: #cf8a78; background: rgba(176, 90, 70, 0.1); }

  .rename-inline { display: flex; gap: 6px; align-items: center; }
  .text-input {
    min-width: 0; width: 180px; padding: 5px 9px;
    font-family: var(--bb-font-mono); font-size: 12px;
    border: 1px solid var(--rule); border-radius: 7px;
    background: var(--bb-bg-1, #16130f); color: var(--bb-white);
  }
  .text-input:focus { outline: none; border-color: var(--bb-border-strong); }

  @media (max-width: 900px) {
    .lane-head { display: none; }
    .lane-row {
      grid-template-columns: minmax(0, 1fr) auto;
      grid-template-areas:
        'id actions'
        'subject subject';
      row-gap: 8px;
    }
    .lane-id { grid-area: id; }
    .lane-actions { grid-area: actions; }
    .lsubject { grid-area: subject; }
    .lane-row .num { display: none; }
  }
</style>
