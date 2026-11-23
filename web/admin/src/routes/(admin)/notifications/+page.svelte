<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { tick } from 'svelte';
  import { SvelteSet } from 'svelte/reactivity';
  import { enhance, type SubmitFunction } from '$app/forms';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import DeckLayout from '@bagel/ui/svelte/DeckLayout.svelte';
  import InspectorSurface from '@bagel/ui/svelte/InspectorSurface.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import { createInspector } from '@bagel/ui/svelte/inspector';
  import { createDiscardGuard } from '@bagel/ui/svelte/discard-guard';
  import { toast } from '@bagel/ui/svelte/toast';
  import { actionPayload, adminToastFailure, type AdminActionOk } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { PageProps } from './$types';
  import { allows } from '#lib/access';
  import { applyStreamed } from '#lib/streamed-load';
  import type { NotificationWire } from '#lib/server/services';
  import FilterTabs from '#lib/components/FilterTabs.svelte';
  import ComposeEditor from '#lib/components/notifications/ComposeEditor.svelte';
  import NotificationDetail from '#lib/components/notifications/NotificationDetail.svelte';
  import NotificationHistory from '#lib/components/notifications/NotificationHistory.svelte';
  import {
    AUDIENCE_FILTERS,
    NEW_NOTIFICATION,
    blankCompose,
    composeComplete,
    type ComposeDraft,
    type HistoryResult
  } from '#lib/components/notifications/notification-compose';

  let { data }: PageProps = $props();

  const { t, locale } = getI18n();
  const numberFormat = new Intl.NumberFormat(locale);
  const failed = adminToastFailure(toast);
  const canSend = $derived(allows(data.role ?? 'moderator', 'notifications.send'));

  let settled = $state.raw<{ page: number; result: HistoryResult }>();
  let audience = $state('all');
  let selectedId = $state<number>();
  const retracted = new SvelteSet<number>();
  const history = $derived(settled?.page === data.page ? settled.result : undefined);
  const notifications = $derived(history?.notifications.filter(({ id }) => !retracted.has(id)) ?? []);
  const rows = $derived(audience === 'all' ? notifications : notifications.filter((row) => row.scope === audience));
  const selected = $derived(rows.find(({ id }) => id === selectedId));
  const audienceOptions = $derived.by(() => {
    const counts = { all: notifications.length, broadcast: 0, direct: 0 };
    for (const notification of notifications) counts[notification.scope]++;
    return AUDIENCE_FILTERS.map(({ value, label }) => ({
      value,
      label: t(label),
      count: history?.degraded === false ? numberFormat.format(counts[value]) : '\u00a0'
    }));
  });

  $effect(() => {
    const page = data.page;
    applyStreamed(data.history, (result) => {
      if (settled?.page !== page) selectedId = undefined;
      settled = { page, result };
    });
  });

  const inspector = createInspector<ComposeDraft>();
  const discard = createDiscardGuard(() => inspector.dirty);
  let retractForm = $state<HTMLFormElement>();
  let retractTarget = $state<NotificationWire | null>(null);
  let retracting = $state(false);
  const drafting = $derived(inspector.selectedId === NEW_NOTIFICATION);
  const draft = $derived(drafting ? inspector.draft : null);
  const complete = $derived(draft !== null && composeComplete(draft));
  const busy = $derived(inspector.status === 'saving' || retracting);
  const surfaceId = 'notification-inspector';
  let opener: HTMLElement | undefined;

  function focusInspector() {
    if (window.matchMedia('(max-width: 1079px)').matches) return;
    void tick().then(() => document.getElementById(surfaceId)?.parentElement?.focus());
  }

  function finish() {
    inspector.reset();
    selectedId = undefined;
    const trigger = opener;
    opener = undefined;
    void tick().then(() => {
      if (trigger?.isConnected) trigger.focus();
      else document.getElementById('notification-filters')?.querySelector<HTMLElement>('[tabindex="0"]')?.focus();
    });
  }

  function setAudience(value: string) {
    audience = value;
    if (!selected) selectedId = undefined;
  }

  function openCompose(event: MouseEvent & { currentTarget: HTMLButtonElement }) {
    if (!canSend || busy || drafting) return;
    opener = event.currentTarget;
    selectedId = undefined;
    inspector.open(NEW_NOTIFICATION, blankCompose());
    focusInspector();
  }

  function openNotification(notification: NotificationWire) {
    if (busy) return;
    if (selected?.id === notification.id) {
      close();
      return;
    }
    const trigger = document.getElementById(`notification-row-${notification.id}`)?.querySelector('button');
    discard.guard(() => {
      opener = trigger ?? undefined;
      inspector.reset();
      selectedId = notification.id;
      focusInspector();
    });
  }

  function close() {
    if (!busy) discard.guard(finish);
  }

  const sendSubmit: SubmitFunction = ({ cancel, formData }) => {
    const request = canSend && complete ? inspector.beginSave() : null;
    if (!request) {
      cancel();
      return;
    }

    const expiresAt = Date.parse(request.snapshot.expiresAt);
    if (Number.isFinite(expiresAt)) formData.set('expires_at', new Date(expiresAt).toISOString());

    return async ({ result, update }) => {
      const payload = actionPayload<AdminActionOk>(result);
      const ok = payload?.action?.ok === true;
      inspector.resolved(request.requestId, { type: ok ? 'success' : 'error' });
      if (!ok) {
        failed(payload, t('admin.notifications.sendFailed'));
        return;
      }
      toast('success', payload.action?.notice ?? t('admin.notifications.sent'));
      await update({ reset: false, navigate: false });
      finish();
    };
  };

  const retractSubmit: SubmitFunction = ({ cancel }) => {
    if (!canSend || !retractTarget || busy) {
      cancel();
      return;
    }

    const targetId = retractTarget.id;
    retracting = true;

    return async ({ result, update }) => {
      const payload = actionPayload<AdminActionOk>(result);
      retracting = false;
      retractTarget = null;
      if (payload?.action?.ok !== true) {
        failed(payload, t('admin.notifications.retractFailed'));
        return;
      }
      retracted.add(targetId);
      await update({ reset: false, navigate: false });
      finish();
      toast('success', payload.action.notice ?? t('admin.notifications.retracted'));
    };
  };
</script>

<section class="screen active">
  <PageHead description={t('admin.notifications.description')}>
    {t('admin.notifications.titlePre')}<em>{t('admin.notifications.titleEm')}</em>
  </PageHead>

  <Stack gap={3}>
    <FilterTabs
      id="notification-filters"
      label={t('admin.notifications.filterLabel')}
      options={audienceOptions}
      countWidth="3ch"
      value={audience}
      onValueChange={setAudience}
    />
    {#if canSend}
      <Cluster justify="end" gap={2}>
        <Button style="inline-size: 7.5rem" onclick={openCompose} disabled={busy} aria-expanded={drafting}
          aria-controls={drafting ? surfaceId : undefined}>
          <bb-fit>{t('admin.notifications.compose')}</bb-fit>
        </Button>
      </Cluster>
    {/if}

    <DeckLayout inspecting={!!draft || !!selected} width="380px">
      <NotificationHistory
        {history}
        {rows}
        page={data.page}
        filtered={audience !== 'all'}
        selectedId={selected?.id}
        controls={surfaceId}
        onSelect={openNotification}
      />

      <InspectorSurface
        open={!!draft || !!selected}
        title={t(selected ? 'admin.notifications.detailTitle' : 'admin.notifications.composeTitle')}
        tabindex={-1}
        controls={surfaceId}
        closeLabel={t('admin.close')}
        onClose={close}
      >
        {#if selected}
          <NotificationDetail
            notification={selected}
            {canSend}
            {busy}
            onRetract={() => (retractTarget = selected ?? null)}
          />
        {:else if draft}
          <ComposeEditor
            {draft}
            status={inspector.status}
            dirty={inspector.dirty}
            canSave={complete}
            onChange={inspector.edit}
            onCancel={close}
            onSubmit={sendSubmit}
          />
        {/if}
      </InspectorSurface>
    </DeckLayout>
  </Stack>
</section>

<ConfirmDialog
  open={retractTarget !== null}
  title={t('admin.notifications.confirmRetractTitle')}
  body={t('admin.notifications.confirmRetractBody')}
  confirmLabel={t('admin.notifications.retract')}
  cancelLabel={t('common.cancel')}
  busy={retracting}
  onCancel={() => (retractTarget = null)}
  onConfirm={() => retractForm?.requestSubmit()}
  tone="danger"
/>
<form method="POST" action="?/delete" use:enhance={retractSubmit} bind:this={retractForm} hidden>
  <input type="hidden" name="id" value={retractTarget?.id ?? ''} />
</form>

<ConfirmDialog
  open={discard.open}
  title={t('admin.unsaved')}
  confirmLabel={t('common.done')}
  cancelLabel={t('common.cancel')}
  onConfirm={discard.confirm}
  onCancel={discard.cancel}
/>
