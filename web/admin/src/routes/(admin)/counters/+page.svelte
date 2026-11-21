<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { tick } from 'svelte';
  import { MediaQuery } from 'svelte/reactivity';
  import { enhance, type SubmitFunction } from '$app/forms';
  import { invalidate } from '$app/navigation';
  import { actionPayload, adminToastFailure } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { formatCounterValue, parseCounterValue } from '@bagel/kit/validation';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import DeckLayout from '@bagel/ui/svelte/DeckLayout.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import InspectorSurface from '@bagel/ui/svelte/InspectorSurface.svelte';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import SwapStack from '@bagel/ui/svelte/SwapStack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { createDiscardGuard } from '@bagel/ui/svelte/discard-guard';
  import { createInspector } from '@bagel/ui/svelte/inspector';
  import { toast } from '@bagel/ui/svelte/toast';
  import StatePill from '#lib/components/StatePill.svelte';
  import CounterEditor from '#lib/components/counters/CounterEditor.svelte';
  import {
    NEW_COUNTER, COUNTERS_DATA, blankCounter, counterComplete, counterName, type CounterDraft
  } from '#lib/components/counters/counter-draft';
  import type { BotCounter } from '#lib/server/services';
  import { applyStreamed } from '#lib/streamed-load';
  import type { BotCountersBundle, CounterAction } from './+page.server';

  let { data } = $props();

  const { t, locale } = getI18n();
  const collator = new Intl.Collator(locale);
  const numberFormat = new Intl.NumberFormat(locale);
  const failed = adminToastFailure(toast);
  const inspector = createInspector<CounterDraft>();
  const discard = createDiscardGuard(() => inspector.dirty);
  const compact = new MediaQuery('(max-width: 1079px)');

  let received = $state.raw<{ source: Promise<BotCountersBundle>; bundle: BotCountersBundle }>();
  let search = $state('');
  let searchInput = $state<HTMLInputElement>();
  let busy = $state(false);
  let deleteTarget = $state.raw<BotCounter | null>(null);
  let deleteForm = $state<HTMLFormElement>();
  let opener: Element | null = null;

  const bundle = $derived(received?.source === data.bundle ? received.bundle : undefined);
  const query = $derived(search.trim().toLowerCase());
  const sorted = $derived([...(bundle?.counters ?? [])].sort((a, b) => collator.compare(a.name, b.name)));
  const rows = $derived(query ? sorted.filter((counter) => counter.name.toLowerCase().includes(query)) : sorted);
  const count = $derived(rows.length === 1
    ? t('admin.counters.countOne')
    : t('admin.counters.count', { n: numberFormat.format(rows.length) }));
  const creating = $derived(inspector.selectedId === NEW_COUNTER);
  const selected = $derived(bundle?.counters.find((counter) => counter.name === inspector.selectedId));
  const degraded = $derived(bundle?.degraded === true);
  const confirmationOpen = $derived(deleteTarget !== null || discard.open);
  const inspectorVisible = $derived(inspector.isOpen && (!compact.current || !confirmationOpen));

  $effect(() => {
    const source = data.bundle;
    applyStreamed(source, (bundle) => { received = { source, bundle }; });
  });

  function updateRows(update: (rows: BotCounter[]) => BotCounter[]) {
    if (!bundle || !received) return;
    received = { ...received, bundle: { ...bundle, counters: update(bundle.counters) } };
  }

  function enterEditor(counter?: BotCounter) {
    if (busy) return;
    const id = counter?.name ?? NEW_COUNTER;
    if (inspector.selectedId === id) return close();
    const trigger = document.activeElement;
    discard.guard(() => {
      opener = trigger;
      const draft = counter ? { name: counter.name, value: counter.value } : blankCounter();
      inspector.open(id, draft);
      if (!compact.current) focusDraft();
    });
  }

  function focusDraft() {
    void tick().then(() => document.getElementById('counter-field')?.focus());
  }

  function focusAfterUpdate(trigger: Element | null, fallback?: HTMLElement) {
    void tick().then(() => {
      const valid = trigger instanceof HTMLElement && trigger.isConnected && trigger !== document.body;
      const target = valid ? trigger : fallback;
      target?.focus({ preventScroll: true });
    });
  }

  function releaseEditor() {
    inspector.reset();
    focusAfterUpdate(opener, searchInput);
    opener = null;
  }

  function close() {
    if (!busy) discard.guard(releaseEditor);
  }

  function applySave(snapshot: CounterDraft, created?: BotCounter) {
    if (creating) {
      const name = counterName(snapshot.name) ?? snapshot.name;
      const counter = created ?? bundle?.counters.find((row) => row.name === name) ?? { name, scope: 'bot', value: '0' };
      updateRows((rows) => [...rows.filter((row) => row.name !== counter.name), counter]);
      releaseEditor();
      return;
    }
    const value = parseCounterValue(snapshot.value) ?? snapshot.value;
    inspector.rebase({ value });
    updateRows((rows) => rows.map((row) => row.name === snapshot.name ? { ...row, value } : row));
    focusDraft();
  }

  function retry() {
    searchInput?.focus();
    void invalidate(COUNTERS_DATA);
  }

  const saveSubmit: SubmitFunction = ({ cancel }) => {
    if (busy || !inspector.draft || !counterComplete(inspector.draft)) return cancel();
    const begun = inspector.beginSave();
    if (!begun) return cancel();
    busy = true;
    return async ({ result }) => {
      busy = false;
      const payload = actionPayload<CounterAction>(result);
      const ok = result.type === 'success' && payload?.ok === true;
      inspector.resolved(begun.requestId, { type: ok ? 'success' : 'error' });
      if (!ok) {
        failed(payload, t('admin.counters.saveFailed'));
        focusDraft();
        return;
      }
      toast('success', t(creating ? 'admin.counters.created' : 'admin.counters.updated'));
      applySave(begun.snapshot, payload?.counter);
    };
  };

  const deleteSubmit: SubmitFunction = ({ cancel }) => {
    if (!deleteTarget || busy) return cancel();
    const { name } = deleteTarget;
    const trigger = document.activeElement;
    busy = true;
    return async ({ result }) => {
      busy = false;
      const payload = actionPayload(result);
      if (result.type !== 'success' || !payload?.ok) {
        failed(payload, t('admin.counters.deleteFailed'));
        focusAfterUpdate(trigger);
        return;
      }
      deleteTarget = null;
      updateRows((rows) => rows.filter((row) => row.name !== name));
      releaseEditor();
      toast('success', t('admin.counters.deleted'));
    };
  };
</script>

<section class="screen active">
  <PageHead description={t('admin.counters.description')}>
    {t('admin.counters.titlePre')}<em>{t('admin.counters.titleEm')}</em>
  </PageHead>

  <PageToolbar>
    {#snippet leading()}
      <div class="search">
        <SearchInput
          fill
          bind:value={search}
          bind:element={searchInput}
          placeholder={t('admin.counters.search')}
          aria-label={t('admin.counters.search')}
        />
      </div>
    {/snippet}
    {#snippet trailing()}
      <SwapStack alternate={degraded} style="inline-size: 9.25rem; max-inline-size: 100%">
        {#snippet primary()}
          <Button
            block
            onclick={() => enterEditor()}
            disabled={!bundle || busy}
            aria-expanded={creating && inspectorVisible}
            aria-controls={creating && inspectorVisible ? 'counter-inspector' : undefined}
          >
            <bb-fit><span data-fit>{t('admin.counters.add')}</span></bb-fit>
          </Button>
        {/snippet}
        {#snippet secondary()}
          <Button block variant="secondary" onclick={retry}>
            <bb-fit><span data-fit>{t('common.retry')}</span></bb-fit>
          </Button>
        {/snippet}
      </SwapStack>
    {/snippet}
  </PageToolbar>

  <DeckLayout inspecting={inspector.isOpen} width="min(380px, 49%)">
    <DeckList as="section" style="min-block-size: 10.5rem" aria-label={t('admin.counters.listLabel')} aria-busy={!bundle}>
      <div class="list-count">
        <div class="count-slot">
          <Text as="div" size="xs" tone="muted" mono role="status">
            <bb-fit><span data-fit>{bundle && !degraded ? count : ''}</span></bb-fit>
          </Text>
        </div>
        {#if !bundle}
          <div class="count-slot" aria-hidden="true"><Skeleton width="6rem" /></div>
        {/if}
      </div>
      {#if !bundle}
        <div aria-hidden="true">
          {#each [0, 1] as index (index)}
            <ManagementRow selectable={false}>
              {#snippet primary()}
                <div class="row-skeleton">
                  <div class="row-skeleton-text">
                    <Skeleton width="min(15ch, 90%)" height="20px" variant="block" />
                    <Skeleton width="min(9ch, 60%)" height="16px" variant="block" />
                  </div>
                  <Skeleton width="4.5rem" variant="pill" />
                </div>
              {/snippet}
            </ManagementRow>
          {/each}
        </div>
      {:else if degraded}
        <div class="list-alert">
          <AlertBanner tone="danger">
            {t('admin.counters.degraded')}
          </AlertBanner>
        </div>
      {:else if rows.length}
        <ul class="bb-list">
          {#each rows as counter (counter.name)}
            {@const active = inspector.selectedId === counter.name}
            {@const expanded = active && inspectorVisible}
            <ManagementRow
              as="li"
              selected={active}
              {expanded}
              controls={expanded ? 'counter-inspector' : undefined}
              onSelect={() => enterEditor(counter)}
              title={counter.name}
              meta={t('admin.counters.rowMeta', { scope: counter.scope })}
            >
              {#snippet marks()}
                <StatePill tone="neutral">{formatCounterValue(counter.value, locale)}</StatePill>
              {/snippet}
            </ManagementRow>
          {/each}
        </ul>
      {:else if bundle.counters.length}
        <EmptyState title={t('admin.counters.noMatches')} body={t('admin.counters.noMatchesBody')} />
      {:else}
        <EmptyState title={t('admin.counters.empty')} body={t('admin.counters.emptyBody')} />
      {/if}
    </DeckList>

    {#if inspector.isOpen && inspector.draft}
      <InspectorSurface
        open={inspectorVisible}
        title={creating ? t('admin.counters.newTitle') : inspector.draft.name}
        controls="counter-inspector"
        closeLabel={t('admin.close')}
        onClose={close}
      >
        {#key inspector.selectedId}
          <CounterEditor
            draft={inspector.draft}
            {creating}
            scope={selected?.scope}
            status={inspector.status}
            dirty={inspector.dirty}
            {busy}
            onEdit={inspector.edit}
            onCancel={close}
            onDelete={selected ? () => (deleteTarget = selected ?? null) : undefined}
            submit={saveSubmit}
          />
        {/key}
      </InspectorSurface>
    {/if}
  </DeckLayout>
</section>

<ConfirmDialog
  open={deleteTarget !== null}
  title={t('admin.counters.confirmDeleteTitle')}
  body={deleteTarget ? t('admin.counters.confirmDeleteBody', { name: deleteTarget.name }) : undefined}
  confirmLabel={t('common.delete')}
  cancelLabel={t('common.cancel')}
  {busy}
  onCancel={() => (deleteTarget = null)}
  onConfirm={() => deleteForm?.requestSubmit()}
  tone="danger"
/>
<form method="POST" action="?/delete" use:enhance={deleteSubmit} bind:this={deleteForm} hidden>
  <input type="hidden" name="name" value={deleteTarget?.name ?? ''} />
</form>

<ConfirmDialog
  open={discard.open}
  title={t('admin.unsaved')}
  confirmLabel={t('admin.counters.discard')}
  cancelLabel={t('common.cancel')}
  onConfirm={discard.confirm}
  onCancel={discard.cancel}
/>

<style>
  .search {
    flex: 1 1 8rem;
    max-inline-size: 24rem;
    min-inline-size: 0;
  }

  .list-count {
    display: grid;
    align-items: center;
    min-block-size: 2rem;
    padding-inline: var(--bb-space-4);
  }

  .count-slot {
    grid-area: 1 / 1;
    min-inline-size: 0;
  }

  .row-skeleton {
    display: flex;
    align-items: center;
    gap: var(--bb-space-3);
  }

  .row-skeleton-text {
    display: grid;
    gap: 2px;
    flex: 1;
    min-inline-size: 0;
  }

  .list-alert {
    padding: 0 var(--bb-space-4) var(--bb-space-3);
  }

  @media (max-width: 480px) {
    .search {
      flex-basis: 100%;
      max-inline-size: none;
    }
  }
</style>
