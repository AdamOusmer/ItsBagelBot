<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Operator notifications, on the shared deck + inspector.
  //
  // Compose used to be a permanently-open Card above the sent list, so the page
  // opened on an empty form nobody had asked for and the history sat below the
  // fold. Compose is now the inspector's `new` mode and the history is the deck;
  // one surface is open at a time, which is the shape every other management
  // page here already has.
  //
  // The action names (`send`, `delete`) are the server's and are not renamed --
  // the audit trail keys off them.
  import { untrack } from 'svelte';
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import PageHead from '@bagel/kit/components/PageHead.svelte';
  import PageToolbar from '@bagel/kit/components/PageToolbar.svelte';
  import DeckList from '@bagel/kit/components/DeckList.svelte';
  import ManagementRow from '@bagel/kit/components/ManagementRow.svelte';
  import InspectorSurface from '@bagel/kit/components/InspectorSurface.svelte';
  import AlertBanner from '@bagel/kit/components/AlertBanner.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import ConfirmDialog from '@bagel/kit/components/ConfirmDialog.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import { createInspector } from '@bagel/kit/inspector';
  import { createDiscardGuard } from '@bagel/kit/discard-guard';
  import { toast } from '@bagel/kit/toast';
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

  // Streamed history -> local state, so a retract can apply optimistically and
  // roll back on failure.
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

  // ── Inspector: `new` composes, a selection reads ───────────────────────────
  const inspector = createInspector<ComposeDraft>();
  let draft = $state<ComposeDraft | null>(null);
  let busy = $state(false);

  // Push editor changes into the machine for dirty tracking. The spread reads
  // each field so the effect re-runs on any field mutation; the edit itself is
  // untracked because it both reads and writes the machine's state, which would
  // otherwise make the effect depend on state it also mutates (an unsafe cycle).
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
    // A sent notification is read-only, so the machine holds no draft for it;
    // `draft` stays null and the surface renders NotificationDetail instead.
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

  // ── Send ───────────────────────────────────────────────────────────────────
  // No optimistic row: the server assigns the id and resolves a username to a
  // user id, so the sent row is not locally derivable. The page reloads the
  // history instead of guessing at one.
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
      toast('ok', p!.action!.notice ?? t('admin.notifications.sent'));
      if (applied) {
        inspector.reset();
        draft = null;
      }
      // invalidateAll, not a local push: only the server knows the new row's id
      // and its resolved target. The inspector is already closed by here, so the
      // reload costs no editor state.
      await update({ reset: false });
    };
  };

  // ── Retract (confirmed; recipients have already seen it) ───────────────────
  let retractTarget = $state<NotificationWire | null>(null);
  let retractForm = $state<HTMLFormElement | null>(null);

  // Optimistic: the row disappears immediately; a refused delete puts it back
  // with the real error, so the list never lies about what recipients still see.
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
        toast('ok', p.action.notice ?? t('admin.notifications.retracted'));
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
        <span class="count">
          {notifications.length === 1
            ? t('admin.notifications.countOne')
            : t('admin.notifications.count', { n: String(notifications.length) })}
        </span>
      {:else}
        <Skeleton variant="pill" width="130px" />
      {/if}
    {/snippet}
    {#snippet trail()}
      <Button variant="primary" onclick={openCompose}>{t('admin.notifications.compose')}</Button>
    {/snippet}
  </PageToolbar>

  <div class="deck" class:inspecting={inspector.isOpen}>
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
                onselect={() => openNotification(n)}
              >
                {#snippet primary()}
                  <span class="row">
                    <span class="who">
                      <span class="name">{n.title}</span>
                      <span class="meta">
                        {t('admin.notifications.rowMeta', {
                          who: n.created_by_login,
                          when: ago(n.created_at)
                        })}
                      </span>
                    </span>
                    <span class="marks">
                      <StatePill tone={LEVEL_TONE[n.level]}>{t(LEVEL_LABEL[n.level])}</StatePill>
                      <StatePill tone="neutral">{t(audience.key, audience.params)}</StatePill>
                    </span>
                  </span>
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
        <div class="pager">
          <a
            class="bb-btn bb-btn--ghost"
            class:disabled={page <= 1}
            href={pageHref(page - 1)}
            aria-disabled={page <= 1}
          >
            {t('admin.notifications.pagerPrev')}
          </a>
          <span class="pager-label">
            {t('admin.notifications.pagerLabel', {
              page: String(page),
              max: String(maxPages)
            })}
          </span>
          <a
            class="bb-btn bb-btn--ghost"
            class:disabled={!hasMore}
            href={pageHref(page + 1)}
            aria-disabled={!hasMore}
          >
            {t('admin.notifications.pagerNext')}
          </a>
        </div>
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
        <!-- Keyed on the selection so switching rows mounts a FRESH surface: the
             composer binds to the draft snapshot taken at open, so one reused
             instance would carry the previous message's fields. -->
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
  </div>
</section>

<ConfirmDialog
  open={retractTarget !== null}
  title={t('admin.notifications.confirmRetractTitle')}
  body={t('admin.notifications.confirmRetractBody')}
  confirmLabel={t('admin.notifications.retract')}
  cancelLabel={t('common.cancel')}
  danger
  {busy}
  onCancel={() => (retractTarget = null)}
  onConfirm={() => retractForm?.requestSubmit()}
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
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: 13.5px;
    color: var(--bb-white);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .meta {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
  }
  .marks {
    display: flex;
    gap: 6px;
    flex: none;
  }
  @media (max-width: 560px) {
    .marks :global(.pill:last-child) {
      display: none;
    }
  }

  .pager {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 14px;
    padding: 14px;
  }
  .pager-label {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-muted);
  }
  .disabled {
    pointer-events: none;
    opacity: 0.4;
  }
</style>
