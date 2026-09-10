<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Bot-global counters: the reserved loyalty namespace shared across every
  // channel (e.g. the personality module's lifetime tallies). Managers create,
  // set and delete them here; system modules bump them from Go. Broadcasters
  // never see this namespace.
  //
  // On the shared deck + inspector. The previous page was a <table> with a
  // create form above it and a <form> per row inside a cell, so a row carried
  // three interactive controls and the value editor had no dirty state at all.
  // One inspector now serves "create" and "set", which differ only in whether
  // the name is already taken.
  import { untrack } from 'svelte';
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import DeckList from '@bagel/kit/components/DeckList.svelte';
  import ManagementRow from '@bagel/kit/components/ManagementRow.svelte';
  import InspectorSurface from '@bagel/kit/components/InspectorSurface.svelte';
  import AlertBanner from '@bagel/kit/components/AlertBanner.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import ConfirmDialog from '@bagel/kit/components/ConfirmDialog.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import EditorFooter from '@bagel/kit/components/EditorFooter.svelte';
  import { createInspector } from '@bagel/kit/inspector';
  import { createDiscardGuard } from '@bagel/kit/discard-guard';
  import { toast } from '@bagel/kit/toast';
  import { actionPayload, adminToastFailure } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { BotCounter } from '$lib/server/services';
  import StatePill from '$lib/components/StatePill.svelte';
  import {
    NEW_COUNTER,
    blankCounter,
    counterComplete,
    type CounterDraft
  } from '$lib/components/counters/counter-draft';
  import type { BotCountersBundle } from './+page.server';

  let { data } = $props();

  const { t } = getI18n();
  const failed = adminToastFailure(toast);

  // Streamed bundle -> local state. Mutations reconcile locally rather than
  // through invalidateAll: a full re-load would tear the inspector down, and
  // Save is meant to keep it open.
  let counters = $state<BotCounter[]>([]);
  let loaded = $state(false);
  let degraded = $state(false);
  $effect(() => {
    let alive = true;
    loaded = false;
    data.bundle.then((b: BotCountersBundle) => {
      if (!alive) return;
      counters = b.counters;
      degraded = b.degraded;
      loaded = true;
    });
    return () => {
      alive = false;
    };
  });

  const rows = $derived([...counters].sort((a, b) => a.name.localeCompare(b.name)));

  // ── Inspector (shared controller over the pure state machine) ──────────────
  const inspector = createInspector<CounterDraft>();
  let draft = $state<CounterDraft | null>(null);
  let busy = $state(false);

  // Push editor changes into the machine for dirty tracking. The spread reads
  // each field so the effect re-runs on any field mutation; the edit itself is
  // untracked because it both reads and writes the machine's state, which would
  // otherwise make the effect depend on state it also mutates (an unsafe cycle).
  $effect(() => {
    const snap = draft ? { ...draft } : null;
    if (snap) untrack(() => inspector.edit(snap));
  });

  const creating = $derived(inspector.selectedId === NEW_COUNTER);
  const selected = $derived(
    creating ? null : (counters.find((c) => c.name === inspector.selectedId) ?? null)
  );
  const canSave = $derived(inspector.dirty && !!draft && counterComplete(draft));

  const discard = createDiscardGuard(
    () => inspector.dirty,
    () => {
      inspector.reset();
      draft = null;
    }
  );

  function draftOf(c: BotCounter): CounterDraft {
    return { name: c.name, value: c.value };
  }

  function openNew() {
    discard.guard(() => {
      const blank = blankCounter();
      inspector.open(NEW_COUNTER, blank);
      draft = { ...blank };
    });
  }

  function openCounter(c: BotCounter) {
    if (inspector.selectedId === c.name) {
      close();
      return;
    }
    discard.guard(() => {
      inspector.open(c.name, draftOf(c));
      draft = draftOf(c);
    });
  }

  function close() {
    discard.guard(() => {
      inspector.reset();
      draft = null;
    });
  }

  // ── Save: create posts ?/create, an edit posts ?/set ───────────────────────
  // Both answer the shared `{ ok }` skeleton, so one handler covers them; the
  // local list is reconciled from the draft that was accepted rather than
  // refetched, which is what keeps the inspector open and clean after Save.
  function upsert(snapshot: CounterDraft, wasCreating: boolean) {
    if (!wasCreating) {
      counters = counters.map((c) => (c.name === snapshot.name ? { ...c, value: snapshot.value } : c));
      return;
    }
    counters = [...counters, { name: snapshot.name, scope: 'bot', value: snapshot.value }];
  }

  const saveSubmit: SubmitFunction = () => {
    const begun = inspector.beginSave();
    const wasCreating = creating;
    busy = true;
    return async ({ result }) => {
      busy = false;
      const p = actionPayload(result);
      const ok = result.type === 'success' && p?.ok === true;
      // applied is false when the selection moved on during the request, so a
      // late response for one counter never mutates another's editor.
      const applied = begun
        ? inspector.resolved(begun.requestId, { type: ok ? 'success' : 'error' })
        : false;
      if (!ok) {
        failed(p, t('admin.counters.saveFailed'));
        return;
      }
      if (begun) upsert(begun.snapshot, wasCreating);
      toast('ok', wasCreating ? t('admin.counters.created') : t('admin.counters.updated'));
      // A create has no committed row under the sentinel id, so it closes; an
      // edit stays open and clean (Save does not close the inspector).
      if (wasCreating && applied) {
        inspector.reset();
        draft = null;
      }
    };
  };

  // ── Delete (confirmed; the value is not locally recreatable) ───────────────
  let deleteTarget = $state<BotCounter | null>(null);
  let deleteForm = $state<HTMLFormElement | null>(null);

  const deleteSubmit: SubmitFunction = () => {
    const target = deleteTarget;
    busy = true;
    return async ({ result }) => {
      busy = false;
      deleteTarget = null;
      const p = actionPayload(result);
      if (result.type === 'success' && p?.ok) {
        counters = counters.filter((c) => c.name !== target?.name);
        inspector.reset();
        draft = null;
        toast('ok', t('admin.counters.deleted'));
        return;
      }
      failed(p, t('admin.counters.deleteFailed'));
    };
  };
</script>

<section class="screen active">
  <PageHead eyebrow={t('admin.counters.eyebrow')} description={t('admin.counters.description')}>
    {t('admin.counters.titlePre')}<em>{t('admin.counters.titleEm')}</em>
  </PageHead>

  {#if degraded}
    <AlertBanner>{t('admin.counters.degraded')}</AlertBanner>
  {/if}

  <PageToolbar>
    {#snippet lead()}
      {#if loaded}
        <span class="count">
          {rows.length === 1
            ? t('admin.counters.countOne')
            : t('admin.counters.count', { n: String(rows.length) })}
        </span>
      {:else}
        <Skeleton variant="pill" width="110px" />
      {/if}
    {/snippet}
    {#snippet trail()}
      <Button variant="primary" onclick={openNew}>{t('admin.counters.add')}</Button>
    {/snippet}
  </PageToolbar>

  <div class="deck" class:inspecting={inspector.isOpen}>
    <DeckList>
      {#if !loaded}
        <SkeletonStack rows={3} height="56px" />
      {:else if rows.length}
        <ul class="bb-list" aria-label={t('admin.counters.listLabel')}>
          {#each rows as c (c.name)}
            <li>
              <ManagementRow
                selected={inspector.selectedId === c.name}
                expanded={inspector.selectedId === c.name}
                controls="counter-inspector"
                onselect={() => openCounter(c)}
              >
                {#snippet primary()}
                  <span class="row">
                    <span class="who">
                      <span class="name">{c.name}</span>
                      <span class="meta">{t('admin.counters.rowMeta', { scope: c.scope })}</span>
                    </span>
                    <StatePill tone="neutral">{c.value.toLocaleString()}</StatePill>
                  </span>
                {/snippet}
              </ManagementRow>
            </li>
          {/each}
        </ul>
      {:else}
        <EmptyState title={t('admin.counters.empty')} body={t('admin.counters.emptyBody')} />
      {/if}
    </DeckList>

    {#if inspector.isOpen && draft}
      <InspectorSurface
        open
        title={creating ? t('admin.counters.newTitle') : (selected?.name ?? '')}
        controls="counter-inspector"
        closeLabel={t('admin.close')}
        onClose={close}
      >
        <!-- Keyed on the selection so switching rows mounts a FRESH editor: the
             fields bind to the draft snapshot taken at open, so one reused
             instance would freeze them to the first counter opened. -->
        {#key inspector.selectedId}
          <form
            class="editor"
            method="POST"
            action={creating ? '?/create' : '?/set'}
            use:enhance={saveSubmit}
          >
            <input type="hidden" name="name" value={draft.name} />
            <input type="hidden" name="value" value={String(draft.value)} />

            <Scroller fill padding="18px" data-lenis-prevent>
              <div class="body">
                {#if creating}
                  <Field label={t('admin.counters.fieldName')}>
                    <input
                      class="text-input"
                      type="text"
                      maxlength="64"
                      placeholder={t('admin.counters.fieldNamePlaceholder')}
                      bind:value={draft.name}
                    />
                  </Field>
                {:else}
                  <div class="ident">
                    <div class="ident-name">{draft.name}</div>
                    <div class="ident-meta">
                      {t('admin.counters.rowMeta', { scope: selected?.scope ?? 'bot' })}
                    </div>
                  </div>
                {/if}

                <Field label={t('admin.counters.fieldValue')}>
                  <input
                    class="text-input"
                    type="number"
                    step="1"
                    bind:value={draft.value}
                  />
                </Field>
                <p class="note">{t('admin.counters.valueHint')}</p>

                {#if selected}
                  <section class="block">
                    <h3 class="block-label">{t('admin.counters.dangerTitle')}</h3>
                    <Button
                      variant="destructive"
                      disabled={busy}
                      onclick={() => (deleteTarget = selected)}
                    >
                      {t('common.delete')}
                    </Button>
                  </section>
                {/if}
              </div>
            </Scroller>

            <EditorFooter
              status={inspector.status}
              dirty={inspector.dirty}
              {canSave}
              saveLabel={creating ? t('admin.counters.addCta') : t('common.save')}
              cancelLabel={t('common.cancel')}
              savingLabel={t('admin.saving')}
              savedLabel={t('admin.saved')}
              dirtyLabel={t('admin.unsaved')}
              errorLabel={t('admin.saveFailed')}
              onCancel={close}
            />
          </form>
        {/key}
      </InspectorSurface>
    {/if}
  </div>
</section>

<ConfirmDialog
  open={deleteTarget !== null}
  title={t('admin.counters.confirmDeleteTitle')}
  body={deleteTarget ? t('admin.counters.confirmDeleteBody', { name: deleteTarget.name }) : undefined}
  confirmLabel={t('common.delete')}
  cancelLabel={t('common.cancel')}
  danger
  {busy}
  onCancel={() => (deleteTarget = null)}
  onConfirm={() => deleteForm?.requestSubmit()}
/>
<form method="POST" action="?/delete" use:enhance={deleteSubmit} bind:this={deleteForm} hidden>
  <input type="hidden" name="name" value={deleteTarget?.name ?? ''} />
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
  .count {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-muted);
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

  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }
  .who {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    flex: 1;
  }
  .name {
    font-family: var(--bb-font-mono);
    font-weight: 600;
    font-size: 13.5px;
    color: var(--bb-white);
  }
  .meta {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
  }

  .editor {
    display: flex;
    flex-direction: column;
    min-height: 0;
    max-height: 100%;
  }
  .body {
    display: flex;
    flex-direction: column;
    gap: 18px;
  }
  .ident-name {
    font-family: var(--bb-font-mono);
    font-weight: 700;
    font-size: 16px;
    color: var(--bb-white);
  }
  .ident-meta {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-muted);
    margin-top: 2px;
  }
  .block {
    display: flex;
    flex-direction: column;
    gap: 9px;
    align-items: flex-start;
  }
  .block-label {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--bb-muted);
    margin: 0;
  }
  .note {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    line-height: 1.5;
    color: var(--bb-muted);
    margin: 0;
  }
</style>
