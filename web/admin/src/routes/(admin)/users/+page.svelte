<script lang="ts">
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import { goto } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import InspectorSurface from '@bagel/ui/svelte/InspectorSurface.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import { actionPayload, adminToastFailure } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { allows, type AccessKey } from '$lib/access';
  import type { AdminUserWire, AuditEntry, ChannelSubState } from '$lib/server/services';
  import type { GiveawayWinnerWire } from '$lib/server/giveaways';
  import UserRow from '$lib/components/users/UserRow.svelte';
  import UserInspector from '$lib/components/users/UserInspector.svelte';
  import MessageDialog from '$lib/components/users/MessageDialog.svelte';
  import { usersCsv } from '$lib/components/users/csv';
  import { downloadCsv } from '@bagel/ui/lib/csv';
  import { USER_STATES, USER_STATE_LABEL, type UserStateFilter } from '$lib/components/users/user-state';
  import { USER_ACTIONS, type UserActionDef } from '$lib/components/users/user-actions';
  import type { UserDirectory } from './+page.server';

  let { data } = $props();

  const { t } = getI18n();
  const failed = adminToastFailure(toast);
  const can = (key: AccessKey) => allows(data.role, key);

  let dir = $state<UserDirectory | null>(null);
  $effect(() => {
    let alive = true;
    dir = null;
    data.directory.then((d) => {
      if (alive) dir = d;
    });
    return () => {
      alive = false;
    };
  });

  const rows = $derived(dir?.recent ?? []);

  // svelte-ignore state_referenced_locally
  let search = $state(data.search);
  $effect(() => {
    search = data.search;
  });

  const stateFilter = $derived((data.state || 'all') as UserStateFilter);
  const stateLabels = $derived(USER_STATES.map((s) => t(USER_STATE_LABEL[s])));
  const stateLabel = $derived(stateLabels[USER_STATES.indexOf(stateFilter)]);

  function href(params: { q?: string; state?: string; page?: number }): string {
    const p = new URLSearchParams();
    if (params.q) p.set('q', params.q);
    if (params.state && params.state !== 'all') p.set('state', params.state);
    if (params.page && params.page > 1) p.set('page', String(params.page));
    const qs = p.toString();
    return qs ? `/users?${qs}` : '/users';
  }

  function pickState(label: string) {
    const next = USER_STATES[stateLabels.indexOf(label)];
    if (next) goto(href({ q: data.search, state: next }), { keepFocus: true });
  }

  function submitSearch() {
    goto(href({ q: search.trim(), state: stateFilter }), { keepFocus: true });
  }

  let selectedId = $state<string | null>(null);
  let detached = $state<AdminUserWire | null>(null);
  const selected = $derived(
    selectedId === null ? null : (rows.find((u) => String(u.id) === selectedId) ?? detached)
  );

  let tokenPresent = $state<boolean | null>(null);
  let subState = $state<ChannelSubState | null>(null);
  let viewAsUrl = $state('');
  let history = $state<AuditEntry[] | null>(null);
  let historyError = $state('');
  let prizeHistory = $state<GiveawayWinnerWire[] | null>(null);
  let prizeHistoryError = $state('');

  let lookupForm = $state<HTMLFormElement | null>(null);
  let lookupQ = $state('');

  function closeInspector() {
    selectedId = null;
    detached = null;
    viewAsUrl = '';
    prizeHistory = null;
    prizeHistoryError = '';
  }

  function openUser(u: AdminUserWire) {
    if (selectedId === String(u.id)) {
      closeInspector();
      return;
    }
    detached = null;
    selectedId = String(u.id);
    tokenPresent = null;
    subState = null;
    viewAsUrl = '';
    lookupQ = String(u.id);
    queueMicrotask(() => lookupForm?.requestSubmit());
    if (can('audit.read')) loadHistory(String(u.id));
    loadPrizeHistory(String(u.id));
  }

  async function loadPrizeHistory(id: string) {
    prizeHistory = null;
    prizeHistoryError = '';
    try {
      const response = await fetch(`/users/prizes?q=${encodeURIComponent(id)}`);
      const body = (await response.json()) as { awards?: GiveawayWinnerWire[]; error?: string };
      if (!response.ok || body.error) throw new Error(body.error ?? `history fetch failed (${response.status})`);
      prizeHistory = body.awards ?? [];
    } catch (error) {
      prizeHistoryError = error instanceof Error ? error.message : String(error);
      prizeHistory = [];
    }
  }

  async function loadHistory(id: string) {
    history = null;
    historyError = '';
    try {
      const res = await fetch(`/audit/data?q=${encodeURIComponent(id)}`);
      if (!res.ok) throw new Error(`history fetch failed (${res.status})`);
      const body = (await res.json()) as { entries?: AuditEntry[]; error?: string };
      if (body.error) throw new Error(body.error);
      history = (body.entries ?? []).filter((e) => e.target === id).slice(0, 5);
    } catch (e) {
      historyError = (e as Error).message;
      history = [];
    }
  }

  type LookupResult = {
    user?: AdminUserWire;
    tokenPresent?: boolean;
    subState?: ChannelSubState;
    error?: string;
  };
  type ActionPayload = {
    action?: { ok: boolean; notice: string };
    lookup?: LookupResult;
    subState?: ChannelSubState;
    viewAsUrl?: string;
    error?: string;
  };

  const lookupSubmit: SubmitFunction = () => {
    return async ({ result }) => {
      const lk = actionPayload<ActionPayload>(result)?.lookup;
      if (!lk || lk.error) {
        tokenPresent = null;
        subState = { state: 'unknown', error: lk?.error ?? t('admin.users.lookupFailed'), checkedAt: null };
        return;
      }
      if (lk.user) reconcile(lk.user);
      tokenPresent = lk.tokenPresent ?? null;
      subState = lk.subState ?? null;
    };
  };

  function reconcile(u: AdminUserWire) {
    if (!dir) return;
    const i = dir.recent.findIndex((r) => r.id === u.id);
    if (i >= 0) dir.recent[i] = u;
    else if (selectedId === String(u.id)) detached = u;
  }

  let busy = $state<string | null>(null);
  let forms = $state<Record<string, HTMLFormElement | null>>({});
  let pending = $state<UserActionDef | null>(null);

  function applied(p: ActionPayload) {
    toast('ok', p.action?.notice ?? '');
    if (p.lookup?.user) reconcile(p.lookup.user);
    if (p.subState) subState = p.subState;
    if (p.viewAsUrl) viewAsUrl = p.viewAsUrl;
  }

  function submitFor(def: UserActionDef): SubmitFunction {
    return () => {
      busy = def.id;
      const id = selectedId;
      const before = dir ? dir.recent.map((r) => ({ ...r })) : [];
      const beforeDetached = detached ? { ...detached } : null;
      if (def.optimistic && id && dir) {
        const i = dir.recent.findIndex((r) => String(r.id) === id);
        if (i >= 0) dir.recent[i] = def.optimistic(dir.recent[i]);
        if (detached) detached = def.optimistic(detached);
      }
      if (def.id === 'restart') subState = null;
      return async ({ result }) => {
        busy = null;
        const p = actionPayload<ActionPayload>(result);
        if (result.type === 'success' && p?.action?.ok) {
          applied(p);
          if (def.id === 'delete') dropSelected();
          if (def.id === 'clearToken' || def.id === 'reset') tokenPresent = false;
          return;
        }
        if (dir) dir.recent = before;
        detached = beforeDetached;
        failed(p, t('admin.users.actionFailed', { verb: t(def.label) }));
      };
    };
  }

  function dropSelected() {
    if (dir && selectedId) dir.recent = dir.recent.filter((r) => String(r.id) !== selectedId);
    closeInspector();
  }

  function request(def: UserActionDef) {
    if (def.confirm) {
      pending = def;
      return;
    }
    forms[def.id]?.requestSubmit();
  }

  function confirmPending() {
    const def = pending;
    pending = null;
    if (def) forms[def.id]?.requestSubmit();
  }

  let statusForms = $state<Record<string, HTMLFormElement | null>>({});
  let grantOpen = $state(false);
  let grantDate = $state('');

  function requestStatus(status: string) {
    if (!selected || selected.status === status) return;
    if (status === 'paid') {
      grantDate = new Date(Date.now() + 30 * 864e5).toISOString().slice(0, 10);
      grantOpen = true;
      return;
    }
    statusForms[status]?.requestSubmit();
  }

  function statusSubmit(status: string): SubmitFunction {
    return () => {
      busy = 'status';
      return async ({ result }) => {
        busy = null;
        grantOpen = false;
        const p = actionPayload<ActionPayload>(result);
        if (result.type === 'success' && p?.action?.ok) {
          applied(p);
          return;
        }
        failed(p, t('admin.users.actionFailed', { verb: status }));
      };
    };
  }

  const creatorSubmit: SubmitFunction = () => {
    busy = 'creator';
    return async ({ result }) => {
      busy = null;
      const p = actionPayload<ActionPayload>(result);
      if (result.type === 'success' && p?.action?.ok) {
        applied(p);
        return;
      }
      failed(p, t('admin.users.actionFailed', { verb: t('admin.users.creatorTitle') }));
    };
  };

  let msgOpen = $state(false);

  const msgSubmit: SubmitFunction = () => {
    busy = 'message';
    return async ({ result }) => {
      busy = null;
      msgOpen = false;
      const p = actionPayload<ActionPayload>(result);
      if (result.type === 'success' && p?.action?.ok) {
        toast('ok', p.action.notice);
        return;
      }
      failed(p, t('admin.users.sendFailed'));
    };
  };

  function exportCsv() {
    downloadCsv(`users-page${dir?.page ?? 1}.csv`, usersCsv(rows));
  }
</script>

<section class="screen active">
  <PageHead eyebrow={t('admin.users.eyebrow')} description={t('admin.users.description')}>
    {t('admin.users.titlePre')}<em>{t('admin.users.titleEm')}</em>
  </PageHead>

  <PageToolbar>
    {#snippet lead()}
      {#if dir}
        <span class="stats">
          {t('admin.users.stats', {
            total: dir.stats.total_users.toLocaleString(),
            active: dir.stats.active_users.toLocaleString(),
            premium: dir.stats.premium_users.toLocaleString()
          })}
        </span>
      {:else}
        <Skeleton variant="pill" width="220px" />
      {/if}
    {/snippet}
    {#snippet trail()}
      <form
        class="searchbar"
        onsubmit={(e) => {
          e.preventDefault();
          submitSearch();
        }}
      >
        <SearchInput fill bind:value={search} placeholder={t('admin.users.searchPlaceholder')} />
        <Button variant="ghost" type="submit">{t('admin.overview.quickLookupCta')}</Button>
      </form>
      <Button variant="ghost" onclick={exportCsv} disabled={rows.length === 0}>
        {t('admin.users.exportCsv')}
      </Button>
    {/snippet}
  </PageToolbar>

  <div class="filters">
    <SegmentedControl
      options={stateLabels}
      label={t('admin.users.stateFilter')}
      bind:value={() => stateLabel, pickState}
    />
  </div>

  {#if dir?.degraded}
    <AlertBanner>{t('admin.users.degraded')}</AlertBanner>
  {/if}

  <div class="deck" class:inspecting={selected !== null}>
    <DeckList>
      {#if dir === null}
        <SkeletonStack rows={6} height="56px" />
      {:else if rows.length}
        <ul class="bb-list" aria-label={t('admin.users.listLabel')}>
          {#each rows as u (u.id)}
            <li>
              <UserRow
                user={u}
                selected={selectedId === String(u.id)}
                controls="user-inspector"
                onselect={() => openUser(u)}
              />
            </li>
          {/each}
        </ul>
      {:else if data.search}
        <EmptyState title={t('admin.users.emptyMatch')} body={t('admin.users.emptyMatchBody')} />
      {:else if data.state}
        <EmptyState title={t('admin.users.emptyState')} />
      {:else}
        <EmptyState title={t('admin.users.empty')} />
      {/if}

      {#if dir && (dir.page > 1 || dir.hasMore)}
        <div class="pager">
          <ButtonLink
            variant="ghost"
            href={href({ q: data.search, state: data.state, page: dir.page - 1 })}
            aria-disabled={dir.page <= 1}
          >
            {t('admin.users.pagerPrev')}
          </ButtonLink>
          <span class="pager-label">
            {t('admin.users.pagerLabel', { page: String(dir.page), max: String(dir.maxPages) })}
          </span>
          <ButtonLink
            variant="ghost"
            href={href({ q: data.search, state: data.state, page: dir.page + 1 })}
            aria-disabled={!dir.hasMore}
          >
            {t('admin.users.pagerNext')}
          </ButtonLink>
        </div>
      {/if}
    </DeckList>

    {#if selected}
      <InspectorSurface
        open
        title={t('admin.users.inspectorTitle', { login: selected.username })}
        controls="user-inspector"
        closeLabel={t('admin.close')}
        onClose={closeInspector}
      >
        <UserInspector
          user={selected}
          {tokenPresent}
          {subState}
          {viewAsUrl}
          {history}
          {historyError}
          {prizeHistory}
          {prizeHistoryError}
          canReadHistory={can('audit.read')}
          {busy}
          {can}
          onAction={request}
          onStatus={requestStatus}
          onMessage={() => (msgOpen = true)}
          {creatorSubmit}
        />
      </InspectorSurface>
    {/if}
  </div>
</section>

<form method="POST" action="?/lookup" use:enhance={lookupSubmit} bind:this={lookupForm} hidden>
  <input type="hidden" name="q" value={lookupQ} />
</form>

{#each USER_ACTIONS as def (def.id)}
  <form
    method="POST"
    action="?/{def.action}"
    use:enhance={submitFor(def)}
    bind:this={forms[def.id]}
    hidden
  >
    <input type="hidden" name="user_id" value={selected?.id ?? ''} />
    {#if def.id === 'toggleActive'}
      <input type="hidden" name="active" value={selected?.is_active ? 'false' : 'true'} />
    {/if}
  </form>
{/each}

{#each ['free', 'vip', 'paid'] as st (st)}
  <form
    method="POST"
    action="?/setStatus"
    use:enhance={statusSubmit(st)}
    bind:this={statusForms[st]}
    hidden
  >
    <input type="hidden" name="user_id" value={selected?.id ?? ''} />
    <input type="hidden" name="status" value={st} />
    {#if st === 'paid'}<input type="hidden" name="expires_at" value={grantDate} />{/if}
  </form>
{/each}

<ConfirmDialog
  open={pending !== null}
  title={pending ? t(pending.confirm!.title) : ''}
  body={pending && selected
    ? t(pending.confirm!.body, { login: selected.username, id: String(selected.id) })
    : undefined}
  confirmLabel={pending ? t(pending.label) : ''}
  cancelLabel={t('common.cancel')}
  danger={pending?.danger ?? false}
  busy={busy !== null}
  onCancel={() => (pending = null)}
  onConfirm={confirmPending}
/>

<ConfirmDialog
  open={grantOpen}
  title={t('admin.users.grantTitle')}
  body={selected ? t('admin.users.grantBody', { login: selected.username }) : undefined}
  confirmLabel={t('admin.users.grantCta')}
  cancelLabel={t('common.cancel')}
  busy={busy === 'status'}
  onCancel={() => (grantOpen = false)}
  onConfirm={() => statusForms['paid']?.requestSubmit()}
>
  <Field label={t('admin.users.grantEndsOn')}>
    <Input
      fill mono
      type="date"
      bind:value={grantDate}
      min={new Date(Date.now() + 864e5).toISOString().slice(0, 10)}
    />
  </Field>
</ConfirmDialog>

{#if selected}
  <MessageDialog
    bind:open={msgOpen}
    login={selected.username}
    userId={String(selected.id)}
    busy={busy === 'message'}
    onSubmit={msgSubmit}
  />
{/if}

<style>
  .stats {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
  }
  .searchbar {
    display: flex;
    gap: 8px;
    align-items: center;
  }
  .filters {
    margin-bottom: 14px;
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


  @media (max-width: 680px) {
    .searchbar {
      width: 100%;
    }
    }
</style>
