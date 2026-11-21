<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Checkbox from '@bagel/ui/svelte/Checkbox.svelte';
  import { tick, untrack } from 'svelte';
  import { enhance, type SubmitFunction } from '$app/forms';
  import { goto } from '$app/navigation';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import DeckLayout from '@bagel/ui/svelte/DeckLayout.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import InspectorSurface from '@bagel/ui/svelte/InspectorSurface.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Pager from '@bagel/ui/svelte/Pager.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import { actionPayload, adminToastFailure } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { livePoll } from '@bagel/kit/live-poll';
  import { allows, type AccessKey } from '#lib/access';
  import { getLiveCount } from '#lib/live-count-context';
  import type { AdminUserWire, ChannelSubState } from '#lib/server/services';
  import FilterTabs from '#lib/components/FilterTabs.svelte';
  import UserRow from '#lib/components/users/UserRow.svelte';
  import UserInspector from '#lib/components/users/UserInspector.svelte';
  import type { TokenStatus } from '#lib/components/users/UserConnection.svelte';
  import MessageDialog from '#lib/components/users/MessageDialog.svelte';
  import { usersCsv } from '#lib/components/users/csv';
  import { EMPTY_HISTORY, inspectAudit, inspectPrizes } from '#lib/components/users/user-history';
  import { downloadCsv } from '@bagel/ui/lib/csv';
  import { USER_STATES, USER_STATE_LABEL, type UserStateFilter } from '#lib/components/users/user-state';
  import { USER_ACTIONS, type UserActionDef } from '#lib/components/users/user-actions';
  import type { UserDirectory } from '#lib/server/user-directory';

  let { data } = $props();

  const { t, locale } = getI18n();
  const numberFormat = new Intl.NumberFormat(locale);
  const liveCounter = getLiveCount();
  const liveCount = $derived(liveCounter.value);
  const liveNow = $derived(t('admin.users.liveNow'));
  const liveLabel = $derived(liveCount === null ? liveNow : `${liveNow} ${numberFormat.format(liveCount)}`);
  const failed = adminToastFailure(toast);
  const can = (key: AccessKey) => allows(data.role, key);
  const LIVE_REFRESH_MS = 5_000;

  type DirectoryParams = Pick<typeof data, 'state' | 'order' | 'live' | 'page'> & { q: string };
  type DirectoryRefresh = { directory: UserDirectory | null; liveCount: number | null };

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
  const stats = $derived(dir?.degraded ? null : dir?.stats);
  const currentPage = $derived(dir?.page ?? 1);
  const maxPages = $derived(dir?.maxPages ?? 1);
  const showJump = $derived(maxPages > 5);
  const pagerLabel = $derived.by(() => {
    if (!dir) return t('common.loading');
    if (dir.degraded) return t('admin.overview.kpi.unavailable');
    return t('admin.users.pagerLabel', {
      page: numberFormat.format(currentPage),
      max: numberFormat.format(maxPages)
    });
  });

  let search = $state(untrack(() => data.search));
  $effect(() => {
    search = data.search;
  });

  const stateFilter = $derived(data.state || 'all');
  const stateOptions = $derived(USER_STATES.map((state) => {
    const count = state === 'all' ? stats?.total_users : stats?.directory_counts?.[state];
    return { value: state, label: t(USER_STATE_LABEL[state]), count: count == null ? '' : numberFormat.format(count) };
  }));

  const orderOptions = $derived([
    { value: 'newest', label: t('admin.users.orderNewest') },
    { value: 'oldest', label: t('admin.users.orderOldest') }
  ]);

  function href(overrides: Partial<DirectoryParams> = {}, path = '/users'): string {
    const params = { q: data.search, state: data.state, order: data.order, live: data.live, page: 1, ...overrides };
    const p = new URLSearchParams();
    if (params.q) p.set('q', params.q);
    if (params.state && params.state !== 'all') p.set('state', params.state);
    if (params.order === 'oldest') p.set('order', params.order);
    if (params.live) p.set('live', '1');
    if (params.page > 1) p.set('page', String(params.page));
    const qs = p.toString();
    return qs ? `${path}?${qs}` : path;
  }

  function navigate(overrides: Partial<DirectoryParams>) {
    void goto(href(overrides), { reset: false });
  }

  function toggleState(state: UserStateFilter) {
    const next = stateFilter === state ? 'all' : state;
    if (next !== stateFilter) navigate({ state: next });
  }

  function submitSearch(event: SubmitEvent) {
    event.preventDefault();
    navigate({ q: search.trim() });
  }

  function submitJump(event: SubmitEvent & { currentTarget: HTMLFormElement }) {
    event.preventDefault();
    const page = Number(new FormData(event.currentTarget).get('page'));
    navigate({ page });
  }

  let selectedId = $state<string | null>(null);
  let detached = $state<AdminUserWire | null>(null);
  const selected = $derived(
    selectedId === null ? null : (rows.find((u) => String(u.id) === selectedId) ?? detached)
  );

  let tokenStatus = $state<TokenStatus>('checking');
  let subState = $state<ChannelSubState | null>(null);
  let viewAsUrl = $state('');
  let audit = $state.raw<Awaited<ReturnType<typeof inspectAudit>>>(EMPTY_HISTORY);
  let prizes = $state.raw<Awaited<ReturnType<typeof inspectPrizes>>>(EMPTY_HISTORY);
  let inspectorRequest: AbortController | undefined;

  let lookupForm = $state<HTMLFormElement | null>(null);

  $effect(() => () => inspectorRequest?.abort());

  function closeInspector() {
    inspectorRequest?.abort();
    selectedId = null;
    detached = null;
    viewAsUrl = '';
  }

  function openUser(user: AdminUserWire) {
    const id = String(user.id);
    if (selectedId === id) {
      closeInspector();
      return;
    }
    inspectorRequest?.abort();
    const { signal } = (inspectorRequest = new AbortController());
    detached = null;
    selectedId = id;
    tokenStatus = 'checking';
    subState = null;
    viewAsUrl = '';
    audit = EMPTY_HISTORY;
    prizes = EMPTY_HISTORY;
    loadInspector(id, signal);
  }

  function loadInspector(id: string, signal: AbortSignal) {
    void tick().then(() => {
      if (!signal.aborted) lookupForm?.requestSubmit();
    });
    if (can('audit.read')) void inspectAudit(id, signal).then((next) => {
      if (!signal.aborted) audit = next;
    });
    if (can('giveaways.manage')) void inspectPrizes(id, signal).then((next) => {
      if (!signal.aborted) prizes = next;
    });
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

  const lookupSubmit: SubmitFunction = ({ controller, cancel }) => {
    const signal = inspectorRequest?.signal;
    if (!signal || signal.aborted) return cancel();
    const abortLookup = () => controller.abort();
    signal.addEventListener('abort', abortLookup, { once: true });
    return async ({ result }) => {
      signal.removeEventListener('abort', abortLookup);
      if (signal.aborted) return;
      const lk = actionPayload<ActionPayload>(result)?.lookup;
      if (!lk || lk.error) {
        tokenStatus = 'unavailable';
        subState = { state: 'unknown', error: lk?.error ?? t('admin.users.actionResults.lookupFailed'), checkedAt: null };
        return;
      }
      if (lk.user) reconcile(lk.user);
      tokenStatus = lk.tokenPresent === undefined ? 'unavailable' : lk.tokenPresent ? 'present' : 'absent';
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

  function replaceDirectory(next: UserDirectory) {
    const keep = selected;
    dir = next;
    if (keep && !next.recent.some((u) => u.id === keep.id)) detached = keep;
  }

  async function pollDirectory(url: string, signal: AbortSignal, request: Promise<number | null>): Promise<boolean> {
    if (document.hidden) return false;
    try {
      const res = await fetch(url, { signal });
      if (!res.ok) throw new Error(`directory fetch failed (${res.status})`);
      const next = (await res.json()) as DirectoryRefresh;
      if (signal.aborted) return false;
      liveCounter.update(request, next.liveCount);
      if (next.directory && !next.directory.degraded) replaceDirectory(next.directory);
    } catch {
      if (!signal.aborted) liveCounter.update(request, null);
    }
    return false;
  }

  $effect(() => {
    if (busy || !data.live) return;
    const url = href({ page: data.page }, '/users/directory');
    const request = data.liveCount;
    const abort = new AbortController();
    const stop = livePoll(() => pollDirectory(url, abort.signal, request), {
      firstDelayMs: LIVE_REFRESH_MS,
      delayMs: () => LIVE_REFRESH_MS,
      timeoutMs: Number.POSITIVE_INFINITY,
      refreshOnVisible: true
    });
    return () => {
      stop();
      abort.abort();
    };
  });
  let forms = $state<Record<string, HTMLFormElement | null>>({});
  let pending = $state<UserActionDef | null>(null);

  function applied(p: ActionPayload) {
    toast('success', p.action?.notice ?? '');
    if (p.lookup?.user) reconcile(p.lookup.user);
  }

  function applyUserAction(def: UserActionDef, payload: ActionPayload, userId: string) {
    applied(payload);
    if (def.id === 'delete') dropUser(userId);
    if (selectedId !== userId) return;
    if (payload.subState) subState = payload.subState;
    if (payload.viewAsUrl) viewAsUrl = payload.viewAsUrl;
    if (def.id === 'clearToken' || def.id === 'reset') tokenStatus = 'absent';
  }

  function submitFor(def: UserActionDef): SubmitFunction {
    return ({ formData }) => {
      const userId = String(formData.get('user_id') ?? '');
      const optimistic = def.optimistic;
      const before = selectedId === userId ? selected : null;
      const beforeSubState = subState;
      busy = def.id;
      if (before && optimistic) reconcile(optimistic(before));
      if (def.id === 'restart') subState = null;
      return async ({ result }) => {
        busy = null;
        const p = actionPayload<ActionPayload>(result);
        if (result.type === 'success' && p?.action?.ok) return applyUserAction(def, p, userId);
        if (before && optimistic) reconcile(before);
        if (def.id === 'restart' && selectedId === userId && subState === null) subState = beforeSubState;
        failed(p, t('admin.users.actionResults.actionFailed', { verb: t(def.label) }));
      };
    };
  }

  function dropUser(userId: string) {
    if (dir) dir.recent = dir.recent.filter((row) => String(row.id) !== userId);
    if (selectedId === userId) closeInspector();
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
  let grantNotify = $state(false);

  function requestStatus(status: string) {
    if (!selected || selected.status === status) return;
    if (status === 'paid') {
      grantDate = new Date(Date.now() + 30 * 864e5).toISOString().slice(0, 10);
      grantNotify = false;
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
        failed(p, t('admin.users.actionResults.actionFailed', { verb: status }));
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
      failed(p, t('admin.users.actionResults.actionFailed', { verb: t('admin.users.creatorTitle') }));
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
        toast('success', p.action.notice);
        return;
      }
      failed(p, t('admin.users.actionResults.sendFailed'));
    };
  };

  function exportCsv() {
    downloadCsv(`users-page${dir?.page ?? 1}.csv`, usersCsv(rows));
  }
</script>

<section class="screen active users-directory">
  <PageHead eyebrow={t('admin.users.eyebrow')} description={t('admin.users.description')}>
    {t('admin.users.titlePre')}<em>{t('admin.users.titleEm')}</em>
  </PageHead>

  <Stack gap={3}>
    <FilterTabs
      options={stateOptions}
      value={stateFilter}
      label={t('admin.users.stateFilter')}
      countWidth="6ch"
      onValueChange={(state) => toggleState(state as UserStateFilter)}
    />
    <PageToolbar>
      {#snippet leading()}
        <Cluster gap={2} style="min-inline-size: 0;">
          <form class="searchbar" onsubmit={submitSearch}>
            <SearchInput
              fill
              tall
              bind:value={search}
              placeholder={t('admin.users.searchPlaceholder')}
              aria-label={t('admin.users.searchPlaceholder')}
            />
          </form>
          <Cluster gap={2} nowrap>
            <Switch
              label={liveLabel}
              checked={data.live}
              onCheckedChange={(live) => navigate({ live })}
            />
            <Text as="span" size="sm" aria-hidden="true" style="inline-size: 9rem;">
              <bb-fit>{liveLabel}</bb-fit>
            </Text>
          </Cluster>
          <div style="--input-w: 8rem;">
            <Select
              options={orderOptions}
              label={t('admin.users.orderFilter')}
              aria-label={t('admin.users.orderFilter')}
              value={data.order}
              onchange={({ currentTarget }) => navigate({ order: currentTarget.value === 'oldest' ? 'oldest' : 'newest' })}
            />
          </div>
        </Cluster>
      {/snippet}
      {#snippet trailing()}
        <Button variant="secondary" onclick={exportCsv} disabled={rows.length === 0} style="--btn-w: 9rem; --btn-justify: center;">
          <bb-fit>{t('admin.users.exportCsv')}</bb-fit>
        </Button>
      {/snippet}
    </PageToolbar>
  </Stack>

  {#if dir?.degraded}
    <AlertBanner>{t('admin.users.degraded')}</AlertBanner>
  {/if}

  <DeckLayout inspecting={selected !== null} width="380px">
    <DeckList aria-busy={dir === null}>
      {#if dir === null}
        <SkeletonStack rows={6} height="48px" />
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
      {:else if data.live && !dir.degraded}
        <EmptyState title={t('admin.users.emptyLive')} />
      {:else if data.state}
        <EmptyState title={t('admin.users.emptyState')} />
      {:else}
        <EmptyState title={t('admin.users.empty')} />
      {/if}

      <Pager
        fixed fit
        label={pagerLabel}
        aria-busy={dir === null}
        prevHref={href({ page: currentPage - 1 })}
        nextHref={href({ page: currentPage + 1 })}
        hasPrev={currentPage > 1}
        hasNext={currentPage < maxPages}
        prevLabel={t('admin.users.pagerPrev')}
        nextLabel={t('admin.users.pagerNext')}
      />
      <Cluster class="pager-detail" justify="center">
        <Text class="match-count" as="span" size="xs" tone="muted" mono>
          <bb-fit>{dir?.total != null ? t('admin.users.pagerTotal', { total: numberFormat.format(dir.total) }) : ''}</bb-fit>
        </Text>
        <form class="pager-jump" class:unavailable={!showJump} inert={!showJump} onsubmit={submitJump}>
          <Input
            class="jump-input"
            type="number"
            min={1}
            max={maxPages}
            step={1}
            required
            mono
            name="page"
            value={currentPage}
            aria-label={t('admin.users.pagerJump')}
          />
          <Button class="jump-button" variant="secondary" type="submit">
            <bb-fit>{t('admin.users.pagerGo')}</bb-fit>
          </Button>
        </form>
      </Cluster>
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
          {tokenStatus}
          {subState}
          {viewAsUrl}
          history={audit.entries}
          historyError={audit.error}
          prizeHistory={prizes.entries}
          prizeHistoryError={prizes.error}
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
  </DeckLayout>
</section>

<form method="POST" action="?/lookup" use:enhance={lookupSubmit} bind:this={lookupForm} hidden>
  <input type="hidden" name="q" value={selectedId ?? ''} />
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
    {#if st === 'paid'}
      <input type="hidden" name="expires_at" value={grantDate} />
      <input type="hidden" name="notify_email" value="on" disabled={!grantNotify} />
    {/if}
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
  busy={busy !== null}
  onCancel={() => (pending = null)}
  onConfirm={confirmPending}
  tone={pending?.danger ? 'danger' : 'neutral'}
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
  <Checkbox bind:checked={grantNotify}>{t('admin.users.grantEmail.notify')}</Checkbox>
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
  .searchbar {
    inline-size: min(15rem, 100%);
  }

  .pager-jump {
    transition: opacity var(--bb-dur-base) var(--bb-ease-out-expo);
  }

  .unavailable {
    opacity: 0;
  }

  .users-directory :global(.pager-detail) {
    padding: 0 var(--bb-space-3) var(--bb-space-3);
  }

  .users-directory :global(.match-count) {
    inline-size: 16ch;
    text-align: end;
  }

  .pager-jump {
    display: flex;
    align-items: center;
    gap: var(--bb-space-1);
  }

  .pager-jump :global(.jump-input),
  .pager-jump :global(.jump-button) {
    inline-size: 4rem;
    justify-content: center;
  }

  @media (prefers-reduced-motion: reduce) {
    .pager-jump {
      transition: none;
    }
  }
</style>
