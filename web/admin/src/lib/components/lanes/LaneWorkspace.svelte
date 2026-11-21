<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { tick, untrack } from 'svelte';
  import { enhance, type SubmitFunction } from '$app/forms';
  import { actionPayload, adminToastFailure } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import DeckLayout from '@bagel/ui/svelte/DeckLayout.svelte';
  import InspectorSurface from '@bagel/ui/svelte/InspectorSurface.svelte';
  import { createDiscardGuard } from '@bagel/ui/svelte/discard-guard';
  import { createInspector } from '@bagel/ui/svelte/inspector';
  import { toast } from '@bagel/ui/svelte/toast';
  import type { LanesResult, LaneView } from '#lib/server/lanes';
  import LaneDirectory from './LaneDirectory.svelte';
  import LaneEditor from './LaneEditor.svelte';
  import { currentAlias, laneKey, normalizeAlias, type LaneDraft } from './lane-view';

  let { result, rows, canMutate, onRename, onRefresh }: {
    result: LanesResult | null;
    rows: LaneView[];
    canMutate: boolean;
    onRename: (key: string, alias: string) => void;
    onRefresh: () => void;
  } = $props();

  type Operation = 'durable' | 'delete';
  type ActionPayload = { ok?: boolean; notice?: string; error?: string };

  const OPERATIONS = {
    durable: {
      title: 'admin.lanes.confirmDurableTitle',
      body: 'admin.lanes.confirmDurableBody',
      label: 'admin.lanes.makePermanent',
      failure: 'admin.lanes.durableFailed'
    },
    delete: {
      title: 'admin.lanes.confirmDeleteTitle',
      body: 'admin.lanes.confirmDeleteBody',
      label: 'common.delete',
      failure: 'admin.lanes.deleteFailed'
    }
  } as const;
  const CONTROLS = 'lane-inspector';
  const { t } = getI18n();
  const failed = adminToastFailure(toast);
  const inspector = createInspector<LaneDraft>();
  const discard = createDiscardGuard(() => inspector.dirty);

  let draft = $state<LaneDraft | null>(null);
  let busy = $state(false);
  let operation = $state<Operation | null>(null);
  let operationForm: HTMLFormElement;
  let surface = $state<ReturnType<typeof InspectorSurface>>();
  let returnFocus: HTMLButtonElement | null = null;
  const selected = $derived(result?.lanes.find((lane) => laneKey(lane) === inspector.selectedId));
  const canSave = $derived(canMutate && inspector.dirty);
  const confirmation = $derived(operation ? OPERATIONS[operation] : null);

  $effect(() => {
    if (!draft) return;
    const alias = normalizeAlias(draft.alias);
    untrack(() => inspector.edit({ alias }));
  });

  $effect(() => {
    if (inspector.isOpen && !selected && !busy) untrack(reset);
  });

  function reset() {
    inspector.reset();
    draft = null;
    operation = null;
    const target = returnFocus;
    returnFocus = null;
    void tick().then(() => {
      if (inspector.isOpen) return;
      const fallback = document.getElementById('lane-search');
      (target?.isConnected ? target : fallback)?.focus({ preventScroll: true });
    });
  }

  function close() {
    if (!busy) discard.guard(reset);
  }

  function openLane(lane: LaneView, trigger: HTMLButtonElement) {
    if (busy) return;
    if (inspector.selectedId === laneKey(lane)) {
      close();
      return;
    }
    discard.guard(() => {
      const seed = { alias: currentAlias(lane) };
      returnFocus = trigger;
      inspector.open(laneKey(lane), seed);
      draft = { ...seed };
      void tick().then(() => surface?.focusDockedClose());
    });
  }

  const aliasSubmit: SubmitFunction = ({ cancel }) => {
    const key = inspector.selectedId;
    const pending = key && !busy ? inspector.beginSave() : null;
    if (!key || !pending) return cancel();

    busy = true;
    return async ({ result: response }) => {
      busy = false;
      const payload = actionPayload<ActionPayload>(response);
      if (payload?.ok) {
        inspector.resolved(pending.requestId, { type: 'success' });
        onRename(key, pending.snapshot.alias);
        onRefresh();
        toast('success', payload.notice ?? t('admin.lanes.renamed'));
        return;
      }
      inspector.resolved(pending.requestId, { type: 'error' });
      failed(payload, t('admin.lanes.renameFailed'));
    };
  };

  const submitOperation: SubmitFunction = ({ cancel }) => {
    const active = operation;
    if (!active || !selected || busy) return cancel();

    busy = true;
    return async ({ result: response }) => {
      busy = false;
      operation = null;
      const payload = actionPayload<ActionPayload>(response);
      if (!payload?.ok) {
        failed(payload, t(OPERATIONS[active].failure));
        return;
      }
      if (active === 'delete') reset();
      onRefresh();
      toast('success', payload.notice ?? t('admin.lanes.done'));
    };
  };
</script>

<DeckLayout inspecting={selected !== undefined} width="380px">
  <LaneDirectory {result} {rows} selectedId={inspector.selectedId} controls={CONTROLS} onSelect={openLane} />

  {#if selected && draft}
    <InspectorSurface
      open
      title={selected.display}
      controls={CONTROLS}
      bind:this={surface}
      closeLabel={t('admin.close')}
      onClose={close}
    >
      {#key inspector.selectedId}
        <LaneEditor
          bind:draft={() => draft!, (value) => (draft = value)}
          lane={selected}
          {canMutate}
          status={inspector.status}
          dirty={inspector.dirty}
          {canSave}
          {busy}
          onCancel={close}
          onSubmit={aliasSubmit}
          onDurable={() => (operation = 'durable')}
          onDelete={() => (operation = 'delete')}
        />
      {/key}
    </InspectorSurface>
  {/if}
</DeckLayout>

<ConfirmDialog
  open={confirmation !== null}
  title={confirmation ? t(confirmation.title) : ''}
  body={confirmation && selected ? t(confirmation.body, {
    name: selected.display,
    consumer: selected.consumer,
    stream: selected.stream
  }) : undefined}
  confirmLabel={confirmation ? t(confirmation.label) : ''}
  cancelLabel={t('common.cancel')}
  tone={operation === 'delete' ? 'danger' : 'neutral'}
  {busy}
  onCancel={() => (operation = null)}
  onConfirm={() => operationForm.requestSubmit()}
/>
<form method="POST" action={'?/' + operation} use:enhance={submitOperation} bind:this={operationForm} hidden>
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
