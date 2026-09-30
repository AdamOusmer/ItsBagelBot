<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { untrack } from 'svelte';
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
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
  import type { NotificationWire } from '$lib/server/services';
  import StatePill from '$lib/components/StatePill.svelte';
  import ComposeEditor from '$lib/components/notifications/ComposeEditor.svelte';
  import NotificationDetail from '$lib/components/notifications/NotificationDetail.svelte';
  import {
    NEW_NOTIFICATION,
    LEVEL_LABEL,
    LEVEL_TONE,
    audienceOf,
    blankCompose,
    composeComplete,
    type ComposeDraft
  } from '$lib/components/notifications/notification-compose';

  let { data } = $props();

  const { t } = getI18n();
  const failed = adminToastFailure(toast);

  let notifications = $state<NotificationWire[]>([]);
  let loaded = $state(false);
  let degraded = $state(false);
  let page = $state(1);
  let maxPages = $state(25);
  let hasMore = $state(false);
  $effect(() => {
    let alive = true;
    loaded = false;
    data.history.then((h) => {
      if (!alive) return;
      notifications = h.notifications;
      degraded = h.degraded;
      page = h.page;
      maxPages = h.maxPages;
      hasMore = h.hasMore;
      loaded = true;
    });
    return () => {
      alive = false;
    };
  });

  function pageHref(pageNo: number): string {
    return pageNo > 1 ? `/notifications?page=${pageNo}` : '/notifications';
  }

  const inspector = createInspector<ComposeDraft>();
  let draft = $state<ComposeDraft | null>(null);
  let busy = $state(false);

  $effect(() => {
    const snap = draft ? { ...draft } : null;
    if (snap) untrack(() => inspector.edit(snap));
  });

  const composing = $derived(inspector.selectedId === NEW_NOTIFICATION);
  const selected = $derived(
    composing
      ? null
      : (notifications.find((n) => String(n.id) === inspector.selectedId) ?? null)
  );
  const canSend = $derived(inspector.dirty && !!draft && composeComplete(draft));

  const discard = createDiscardGuard(
    () => inspector.dirty,
    () => {
      inspector.reset();
      draft = null;
    }
  );

  function openCompose() {
    discard.guard(() => {
      const blank = blankCompose();
      inspector.open(NEW_NOTIFICATION, blank);
      draft = { ...blank };
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
    });
  }

  function close() {
    discard.guard(() => {
      inspector.reset();
      draft = null;
    });
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
      if (applied) {
        inspector.reset();
        draft = null;
      }
      await update({ reset: false });
    };
  };

  let retractTarget = $state<NotificationWire | null>(null);
  let retractForm = $state<HTMLFormElement | null>(null);

  const retractSubmit: SubmitFunction = () => {
    const target = retractTarget;
    const before = notifications.map((n) => ({ ...n }));
    busy = true;
    if (target) notifications = notifications.filter((n) => n.id !== target.id);
    return async ({ result }) => {
      busy = false;
      retractTarget = null;
      const p = actionPayload<AdminActionOk>(result);
      if (result.type === 'success' && p?.action?.ok) {
        inspector.reset();
        draft = null;
        toast('success', p.action.notice ?? t('admin.notifications.retracted'));
        return;
      }
      notifications = before;
      failed(p, t('admin.notifications.retractFailed'));
    };
  };
</script>

<section class="screen active">
  <PageHead
    eyebrow={t('admin.notifications.eyebrow')}
    description={t('admin.notifications.description')}
  >
    {t('admin.notifications.titlePre')}<em>{t('admin.notifications.titleEm')}</em>
  </PageHead>

  {#if degraded}
    <AlertBanner>{t('admin.notifications.degraded')}</AlertBanner>
  {/if}

  <PageToolbar>
    {#snippet lead()}
      {#if loaded}
        <Text as="span" size="xs" tone="muted" mono>
          {notifications.length === 1
            ? t('admin.notifications.countOne')
            : t('admin.notifications.count', { n: String(notifications.length) })}
        </Text>
      {:else}
        <Skeleton variant="pill" width="130px" />
      {/if}
    {/snippet}
    {#snippet trail()}
      <Button variant="primary" onclick={openCompose}>{t('admin.notifications.compose')}</Button>
    {/snippet}
  </PageToolbar>

  <DeckLayout inspecting={inspector.isOpen} width="380px">
    <DeckList>
      {#if !loaded}
        <SkeletonStack rows={4} height="60px" />
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

      {#if loaded && (page > 1 || hasMore)}
        <Pager
          label={t('admin.notifications.pagerLabel', {
            page: String(page),
            max: String(maxPages)
          })}
          prevHref={pageHref(page - 1)}
          nextHref={pageHref(page + 1)}
          hasPrev={page > 1}
          hasNext={hasMore}
          prevLabel={t('admin.notifications.pagerPrev')}
          nextLabel={t('admin.notifications.pagerNext')}
        />
      {/if}
    </DeckList>

    {#if inspector.isOpen}
      <InspectorSurface
        open
        title={composing ? t('admin.notifications.composeTitle') : (selected?.title ?? '')}
        controls="notification-inspector"
        closeLabel={t('admin.close')}
        onClose={close}
      >
        {#key inspector.selectedId}
          {#if composing && draft}
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
          {:else if selected}
            <NotificationDetail
              notification={selected}
              {busy}
              onRetract={() => (retractTarget = selected)}
            />
          {/if}
        {/key}
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
  @media (max-width: 560px) {
    .audience {
      display: none;
    }
  }
</style>
