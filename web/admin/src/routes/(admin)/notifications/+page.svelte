<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '@bagel/ui/styles/elements/layout.css';
  import { untrack } from 'svelte';
  import { SvelteSet } from 'svelte/reactivity';
  import { enhance, type SubmitFunction } from '$app/forms';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import DeckLayout from '@bagel/ui/svelte/DeckLayout.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import InspectorSurface from '@bagel/ui/svelte/InspectorSurface.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import Pager from '@bagel/ui/svelte/Pager.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import { createInspector } from '@bagel/ui/svelte/inspector';
  import { createDiscardGuard } from '@bagel/ui/svelte/discard-guard';
  import { toast } from '@bagel/ui/svelte/toast';
  import { actionPayload, adminToastFailure, ago, type AdminActionOk } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { NotificationWire } from '#lib/server/services';
  import StatePill from '#lib/components/StatePill.svelte';
  import ComposeEditor from '#lib/components/notifications/ComposeEditor.svelte';
  import NotificationDetail from '#lib/components/notifications/NotificationDetail.svelte';
  import {
    NEW_NOTIFICATION,
    LEVEL_LABEL,
    LEVEL_TONE,
    audienceOf,
    blankCompose,
    composeComplete,
    type ComposeDraft
  } from '#lib/components/notifications/notification-compose';

  let { data } = $props();

  const { t } = getI18n();
  const failed = adminToastFailure(toast);

  function pageHref(pageNo: number): string {
    return pageNo > 1 ? `/notifications?page=${pageNo}` : '/notifications';
  }

  const inspector = createInspector<ComposeDraft>();
  let draft = $state<ComposeDraft | null>(null);
  let selected = $state.raw<NotificationWire | null>(null);
  let busy = $state(false);

  $effect(() => {
    const snap = draft ? { ...draft } : null;
    if (snap) untrack(() => inspector.edit(snap));
  });

  const composing = $derived(inspector.selectedId === NEW_NOTIFICATION);
  const canSend = $derived(inspector.dirty && !!draft && composeComplete(draft));

  function resetInspector() {
    inspector.reset();
    draft = null;
    selected = null;
  }

  const discard = createDiscardGuard(() => inspector.dirty, resetInspector);

  function openCompose() {
    discard.guard(() => {
      const blank = blankCompose();
      inspector.open(NEW_NOTIFICATION, blank);
      draft = { ...blank };
      selected = null;
    });
  }

  function openNotification(n: NotificationWire) {
    if (inspector.selectedId === String(n.id)) {
      close();
      return;
    }
    discard.guard(() => {
      inspector.open(String(n.id), blankCompose());
      draft = null;
      selected = n;
    });
  }

  function close() {
    discard.guard(resetInspector);
  }

  const sendSubmit: SubmitFunction = () => {
    const requestId = inspector.beginSave()?.requestId;
    busy = true;
    return async ({ result, update }) => {
      busy = false;
      const p = actionPayload<AdminActionOk>(result);
      const ok = result.type === 'success' && p?.action?.ok === true;
      const applied = requestId
        ? inspector.resolved(requestId, { type: ok ? 'success' : 'error' })
        : false;
      if (!ok) {
        failed(p, t('admin.notifications.sendFailed'));
        return;
      }
      toast('success', p!.action!.notice ?? t('admin.notifications.sent'));
      if (applied) resetInspector();
      await update({ reset: false });
    };
  };

  let retractTarget = $state<NotificationWire | null>(null);
  let retractForm = $state<HTMLFormElement | null>(null);
  let retracting = $state<number>();
  const retracted = new SvelteSet<number>();

  function visibleNotifications(notifications: NotificationWire[]) {
    return notifications.filter(({ id }) => id !== retracting && !retracted.has(id));
  }

  const retractSubmit: SubmitFunction = ({ cancel }) => {
    if (!retractTarget || busy) {
      cancel();
      return;
    }
    const targetId = retractTarget.id;
    retracting = targetId;
    busy = true;
    return ({ result }) => {
      const p = actionPayload<AdminActionOk>(result);
      if (result.type === 'success' && p?.action?.ok) {
        retracted.add(targetId);
        resetInspector();
        toast('success', p.action.notice ?? t('admin.notifications.retracted'));
      } else {
        failed(p, t('admin.notifications.retractFailed'));
      }
      busy = false;
      retractTarget = null;
      retracting = undefined;
    };
  };
</script>

{#snippet historyPager(page: number, hasMore: boolean, loading = false)}
  <Pager
    label={t('admin.notifications.pagerLabel', { page: String(page) })}
    prevHref={pageHref(page - 1)}
    nextHref={pageHref(page + 1)}
    hasPrev={!loading && page > 1}
    hasNext={hasMore}
    prevLabel={t('admin.notifications.pagerPrev')}
    nextLabel={t('admin.notifications.pagerNext')}
    fixed
    fit
  />
{/snippet}

<section class="screen active">
  <PageHead
    eyebrow={t('admin.notifications.eyebrow')}
    description={t('admin.notifications.description')}
  >
    {t('admin.notifications.titlePre')}<em>{t('admin.notifications.titleEm')}</em>
  </PageHead>

  <PageToolbar>
    {#snippet leading()}
      <div class="history-count" aria-live="polite">
        {#await data.history}
          <Skeleton variant="pill" width="100%" height="1.25rem" />
        {:then history}
          {#if !history.degraded}
            {@const count = visibleNotifications(history.notifications).length}
            <Text as="span" size="xs" tone="muted" mono>
              <bb-fit>{count === 1
                ? t('admin.notifications.countOne')
                : t('admin.notifications.count', { n: String(count) })}</bb-fit>
            </Text>
          {/if}
        {/await}
      </div>
    {/snippet}
    {#snippet trailing()}
      <Button variant="primary" onclick={openCompose}>{t('admin.notifications.compose')}</Button>
    {/snippet}
  </PageToolbar>

  <DeckLayout inspecting={inspector.isOpen} width="380px">
    <DeckList>
      {#await data.history}
        <SkeletonStack rows={4} height="60px" />
        {@render historyPager(data.page, false, true)}
      {:then history}
        {@const notifications = visibleNotifications(history.notifications)}
        {#if history.degraded}
          <AlertBanner>{t('admin.notifications.degraded')}</AlertBanner>
        {:else if notifications.length}
          <ul class="bb-list" aria-label={t('admin.notifications.listLabel')}>
            {#each notifications as n (n.id)}
              {@const audience = audienceOf(n)}
              <li>
                <ManagementRow
                  selected={inspector.selectedId === String(n.id)}
                  expanded={inspector.selectedId === String(n.id)}
                  controls="notification-inspector"
                  onSelect={() => openNotification(n)}
                  title={n.title}
                  meta={t('admin.notifications.rowMeta', {
                    who: n.created_by_login,
                    when: ago(n.created_at)
                  })}
                >
                  {#snippet marks()}
                    <StatePill tone={LEVEL_TONE[n.level]}>{t(LEVEL_LABEL[n.level])}</StatePill>
                    <span class="audience"><StatePill tone="neutral">{t(audience.key, audience.params)}</StatePill></span>
                  {/snippet}
                </ManagementRow>
              </li>
            {/each}
          </ul>
        {:else}
          <EmptyState
            title={t('admin.notifications.empty')}
            body={t('admin.notifications.emptyBody')}
          />
        {/if}
        {@render historyPager(history.page, history.hasMore)}
      {/await}
    </DeckList>

    {#if selected}
      <InspectorSurface
        open
        title={selected.title}
        controls="notification-inspector"
        closeLabel={t('admin.close')}
        onClose={close}
      >
        <NotificationDetail
          notification={selected}
          {busy}
          onRetract={() => (retractTarget = selected)}
        />
      </InspectorSurface>
    {/if}

    {#if composing && draft}
      <InspectorSurface
        open
        title={t('admin.notifications.composeTitle')}
        controls="notification-inspector"
        closeLabel={t('admin.close')}
        onClose={close}
      >
        <ComposeEditor
          bind:draft={
            () => draft!,
            (v) => (draft = v)
          }
          status={inspector.status}
          dirty={inspector.dirty}
          canSave={canSend}
          onCancel={close}
          onSubmit={sendSubmit}
        />
      </InspectorSurface>
    {/if}
  </DeckLayout>
</section>

<ConfirmDialog
  open={retractTarget !== null}
  title={t('admin.notifications.confirmRetractTitle')}
  body={t('admin.notifications.confirmRetractBody')}
  confirmLabel={t('admin.notifications.retract')}
  cancelLabel={t('common.cancel')}
  {busy}
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

<style>
  .history-count {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    align-items: center;
    inline-size: 10rem;
    min-inline-size: 0;
    min-block-size: 1.25rem;
  }

  @media (max-width: 560px) {
    .audience {
      display: none;
    }
  }
</style>
