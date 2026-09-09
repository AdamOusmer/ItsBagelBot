<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The user directory, on the shared deck + inspector.
  //
  // Every mutation is a hidden <form> here rather than a form inside the
  // inspector: one place owns the confirmation, the optimistic apply and the
  // rollback, and the inspector stays presentational. The action names are the
  // server's and are not renamed -- the audit trail keys off them.
  import { enhance } from '$app/forms';
  import { goto } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import PageHead from '@bagel/kit/components/PageHead.svelte';
  import PageToolbar from '@bagel/kit/components/PageToolbar.svelte';
  import SearchInput from '@bagel/kit/components/SearchInput.svelte';
  import SegmentedControl from '@bagel/kit/components/SegmentedControl.svelte';
  import DeckList from '@bagel/kit/components/DeckList.svelte';
  import InspectorSurface from '@bagel/kit/components/InspectorSurface.svelte';
  import AlertBanner from '@bagel/kit/components/AlertBanner.svelte';
  import EmptyState from '@bagel/kit/components/EmptyState.svelte';
  import ConfirmDialog from '@bagel/kit/components/ConfirmDialog.svelte';
  import SkeletonStack from '@bagel/kit/components/SkeletonStack.svelte';
  import Skeleton from '@bagel/kit/components/Skeleton.svelte';
  import Button from '@bagel/kit/components/Button.svelte';
  import { toast } from '@bagel/kit/toast';
  import { actionPayload, adminToastFailure } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { allows, type AccessKey } from '$lib/access';
  import type { AdminUserWire, AuditEntry, ChannelSubState } from '$lib/server/services';
  import UserRow from '$lib/components/users/UserRow.svelte';
  import UserInspector from '$lib/components/users/UserInspector.svelte';
  import MessageDialog from '$lib/components/users/MessageDialog.svelte';
  import { usersCsv } from '$lib/components/users/csv';
  import { downloadCsv } from '$lib/csv';
  import { USER_STATES, USER_STATE_LABEL, type UserStateFilter } from '$lib/components/users/user-state';
  import { USER_ACTIONS, type UserActionDef } from '$lib/components/users/user-actions';
  import type { UserDirectory } from './+page.server';

  let { data } = $props();

  const { t } = getI18n();
  const failed = adminToastFailure(toast);
  const can = (key: AccessKey) => allows(data.role, key);

  // ── Streamed directory -> local optimistic state ───────────────────────────
  // The load streams the directory promise; it resolves into local state so
  // row mutations can apply optimistically and reconcile against the server
  // echo (or roll back on failure) without refetching the page.
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

  // ── Server-driven search + state filter ────────────────────────────────────
  // Both drive URL params so they cover the whole directory, not just the page
  // already loaded.
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

  // ── Selection + probe ──────────────────────────────────────────────────────
  let selectedId = $state<string | null>(null);
  // A lookup echo for a row that is not on the current page (mutated off-page).
  let detached = $state<AdminUserWire | null>(null);
  const selected = $derived(
    selectedId === null ? null : (rows.find((u) => String(u.id) === selectedId) ?? detached)
  );

  // Probe state is honest about being in flight: null = checking, then the real
  // answer. Never rendered as a guess.
  let tokenPresent = $state<boolean | null>(null);
  let subState = $state<ChannelSubState | null>(null);
  let viewAsUrl = $state('');
  let history = $state<AuditEntry[] | null>(null);
  let historyError = $state('');

  let lookupForm = $state<HTMLFormElement | null>(null);
  let lookupQ = $state('');

  function closeInspector() {
    selectedId = null;
    detached = null;
    viewAsUrl = '';
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
  }

  async function loadHistory(id: string) {
    history = null;
    historyError = '';
    try {
      const res = await fetch(`/audit/data?q=${encodeURIComponent(id)}`);
      if (!res.ok) throw new Error(`history fetch failed (${res.status})`);
      const body = (await res.json()) as { entries?: AuditEntry[]; error?: string };
      if (body.error) throw new Error(body.error);
      // The search matches target/detail broadly; keep only rows aimed at this
      // exact user.
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
        // Selection stays; the probe failed and says so.
        tokenPresent = null;
        subState = { state: 'unknown', error: lk?.error ?? t('admin.users.lookupFailed'), checkedAt: null };
        return;
      }
      if (lk.user) reconcile(lk.user);
      tokenPresent = lk.tokenPresent ?? null;
      subState = lk.subState ?? null;
    };
  };

  // Merge a server-echoed user row into the list (and detached selection).
  function reconcile(u: AdminUserWire) {
    if (!dir) return;
    const i = dir.recent.findIndex((r) => r.id === u.id);
    if (i >= 0) dir.recent[i] = u;
    else if (selectedId === String(u.id)) detached = u;
  }

  // ── Optimistic mutation plumbing ───────────────────────────────────────────
  // Apply the expected result instantly, keep a snapshot, then reconcile with
  // the echoed row on success or roll back + toast the real error on failure.
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
      // The EventSub state is genuinely unknown while a reconnect queues.
      if (def.id === 'restart') subState = null;
      return async ({ result }) => {
        busy = null;
        const p = actionPayload<ActionPayload>(result);
        if (result.type === 'success' && p?.action?.ok) {
          applied(p);
          if (def.id === 'delete') dropSelected();
          // A token wipe has no echoed row, so the badge is cleared here rather
          // than optimistically: it only turns off once the server confirms.
          if (def.id === 'clearToken' || def.id === 'reset') tokenPresent = false;
          return;
        }
        // Roll back the optimistic apply: the UI must not keep a state the
        // server refused.
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

  // Destructive or externally-visible verbs park behind a confirmation; the
  // rest fire straight away.
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

  // ── Status change (paid needs a grant end date) ────────────────────────────
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

  // ── Direct notification ────────────────────────────────────────────────────
  // The composer owns its own fields (see MessageDialog); the page keeps only
  // the open flag and the reply handling, which shares this page's toasts.
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
        <SearchInput bind:value={search} placeholder={t('admin.users.searchPlaceholder')} />
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
          <a
            class="btn ghost"
            class:disabled={dir.page <= 1}
            href={href({ q: data.search, state: data.state, page: dir.page - 1 })}
            aria-disabled={dir.page <= 1}
          >
            {t('admin.users.pagerPrev')}
          </a>
          <span class="pager-label">
            {t('admin.users.pagerLabel', { page: String(dir.page), max: String(dir.maxPages) })}
          </span>
          <a
            class="btn ghost"
            class:disabled={!dir.hasMore}
            href={href({ q: data.search, state: data.state, page: dir.page + 1 })}
            aria-disabled={!dir.hasMore}
          >
            {t('admin.users.pagerNext')}
          </a>
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

<!-- Hidden lookup probe: fired on row select to fetch token + EventSub state. -->
<form method="POST" action="?/lookup" use:enhance={lookupSubmit} bind:this={lookupForm} hidden>
  <input type="hidden" name="q" value={lookupQ} />
</form>

<!-- One hidden form per table entry, so the inspector's buttons stay buttons and
     the POST body is identical whichever way the verb was reached. -->
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

<!-- free/vip apply immediately; paid goes through the grant modal for its end date. -->
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
  <label class="field">
    {t('admin.users.grantEndsOn')}
    <input
      class="text-input"
      type="date"
      bind:value={grantDate}
      min={new Date(Date.now() + 864e5).toISOString().slice(0, 10)}
    />
  </label>
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
  .pager .btn.disabled {
    opacity: 0.35;
    pointer-events: none;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin: 12px 0 4px;
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
  }
  .text-input {
    padding: 8px 11px;
    font-family: var(--bb-font-mono);
    font-size: 12.5px;
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-sm);
    background: var(--bb-bg-1, #16130f);
    color: var(--bb-white);
  }
  .text-input:focus {
    outline: none;
    border-color: var(--bb-border-strong);
  }

  @media (max-width: 680px) {
    .searchbar {
      width: 100%;
    }
    .searchbar :global(.search) {
      flex: 1;
    }
  }
</style>
