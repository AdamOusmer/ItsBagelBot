<script lang="ts">
  import { enhance } from '$app/forms';
  import { goto } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Icon,
    Button,
    PageHead,
    PageToolbar,
    SearchInput,
    AlertBanner,
    DeckList,
    EmptyState,
    ConfirmDialog,
    Scroller,
    Skeleton,
    toast
  } from '@bagel/shared';
  import type { AdminUserWire, AuditEntry, ChannelSubState } from '$lib/server/services';
  import type { UserDirectory } from './+page.server';

  let { data } = $props();

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

  // ── State model: one effective state per user, five colors ────────────────
  // Precedence: banned beats inactive beats tier. Tags still show every flag;
  // the dot and filter use the effective state.
  type UserState = 'banned' | 'inactive' | 'vip' | 'paid' | 'free';
  function stateOf(u: AdminUserWire): UserState {
    if (u.banned) return 'banned';
    if (!u.is_active) return 'inactive';
    return (u.status as UserState) ?? 'free';
  }

  const STATES = ['all', 'vip', 'paid', 'free', 'banned', 'inactive'] as const;
  // Server-side filter: the chip drives the ?state= param so it covers the
  // whole directory, not just the loaded page.
  // svelte-ignore state_referenced_locally
  let stateFilter = $state<string>(data.state || 'all');
  $effect(() => {
    stateFilter = data.state || 'all';
  });
  function applyState(next: string) {
    const params = new URLSearchParams();
    if (data.search) params.set('q', data.search);
    if (next !== 'all') params.set('state', next);
    const qs = params.toString();
    goto(qs ? `/users?${qs}` : '/users', { keepFocus: true });
  }

  // ── Client-side sort over the loaded page ──────────────────────────────────
  type SortKey = '' | 'user' | 'id' | 'tier' | 'joined' | 'updated';
  let sortKey = $state<SortKey>('');
  let sortDir = $state<1 | -1>(1);

  function toggleSort(key: SortKey) {
    if (sortKey === key) {
      if (sortDir === 1) sortDir = -1;
      else {
        sortKey = ''; // third click restores the server order (updated desc)
        sortDir = 1;
      }
      return;
    }
    sortKey = key;
    sortDir = 1;
  }

  const TIER_RANK: Record<string, number> = { vip: 3, paid: 2, free: 1 };
  function compare(a: AdminUserWire, b: AdminUserWire): number {
    switch (sortKey) {
      case 'user':
        return a.username.localeCompare(b.username);
      case 'id':
        return a.id - b.id;
      case 'tier':
        return (TIER_RANK[b.status] ?? 0) - (TIER_RANK[a.status] ?? 0);
      case 'joined':
        return (a.created_at ?? '').localeCompare(b.created_at ?? '');
      case 'updated':
        return (a.updated_at ?? '').localeCompare(b.updated_at ?? '');
      default:
        return 0;
    }
  }

  // The state filter is applied server-side; only the sort is local.
  const visible = $derived.by(() => {
    if (!sortKey) return rows;
    return [...rows].sort((a, b) => compare(a, b) * sortDir);
  });

  // ── CSV export of what's on screen (filter + sort applied) ────────────────
  function csvEscape(v: string): string {
    return /[",\n]/.test(v) ? `"${v.replaceAll('"', '""')}"` : v;
  }
  function exportCsv() {
    const header = 'id,username,status,state,active,banned,creator_code,created_at,updated_at';
    const lines = visible.map((u) =>
      [
        String(u.id),
        u.username,
        u.status,
        stateOf(u),
        String(u.is_active),
        String(u.banned),
        u.creator_code ?? '',
        u.created_at ?? '',
        u.updated_at ?? ''
      ]
        .map(csvEscape)
        .join(',')
    );
    const blob = new Blob([[header, ...lines].join('\n')], { type: 'text/csv' });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = `users-page${dir?.page ?? 1}${data.state ? `-${data.state}` : ''}${data.search ? `-${data.search}` : ''}.csv`;
    a.click();
    URL.revokeObjectURL(a.href);
  }

  // ── Selection + probe ──────────────────────────────────────────────────────
  let selectedId = $state<string | null>(null);
  // A lookup echo that is not on the current page (e.g. row mutated off-page).
  let detached = $state<AdminUserWire | null>(null);
  const selected = $derived(
    selectedId === null ? null : (rows.find((u) => String(u.id) === selectedId) ?? detached)
  );

  // Probe state is honest about being in flight: null = checking, then the
  // real answer. Never rendered as a guess.
  let tokenPresent = $state<boolean | null>(null);
  let subState = $state<ChannelSubState | null>(null);
  let viewAsUrl = $state('');
  let viewAsCopied = $state(false);

  let lookupForm = $state<HTMLFormElement | null>(null);
  let lookupQ = $state('');

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
    if (isManager) loadTargetHistory(String(u.id));
  }

  // ── Support: operator actions on this user (managers only) ────────────────
  const isManager = $derived(data.role === 'admin' || data.role === 'owner');
  let targetHistory = $state<AuditEntry[] | null>(null);
  let targetHistoryError = $state('');

  async function loadTargetHistory(id: string) {
    targetHistory = null;
    targetHistoryError = '';
    try {
      const res = await fetch(`/audit/data?q=${encodeURIComponent(id)}`);
      if (!res.ok) throw new Error(`history fetch failed (${res.status})`);
      const body = (await res.json()) as { entries?: AuditEntry[]; error?: string };
      if (body.error) throw new Error(body.error);
      // The search matches target/detail broadly; keep only rows aimed at
      // this exact user.
      targetHistory = (body.entries ?? []).filter((e) => e.target === id).slice(0, 5);
    } catch (e) {
      targetHistoryError = (e as Error).message;
      targetHistory = [];
    }
  }

  // ── Support: direct notification from the inspector ───────────────────────
  let msgOpen = $state(false);
  let msgTitle = $state('');
  let msgBody = $state('');
  let msgLevel = $state('info');
  let msgForm = $state<HTMLFormElement | null>(null);

  function openMessage() {
    msgTitle = '';
    msgBody = '';
    msgLevel = 'info';
    msgOpen = true;
  }

  const msgSubmit: SubmitFunction = () => {
    busyVerb = 'message';
    return async ({ result }) => {
      busyVerb = null;
      msgOpen = false;
      const p = payloadOf(result);
      if (result.type === 'success' && p?.action?.ok) {
        toast('ok', p.action.notice);
        return;
      }
      toast('err', p?.action?.notice ?? p?.error ?? 'send failed');
    };
  };

  function closeInspector() {
    selectedId = null;
    detached = null;
    viewAsUrl = '';
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

  function payloadOf(result: unknown): ActionPayload | undefined {
    const r = result as { type: string; data?: ActionPayload };
    return r.type === 'success' || r.type === 'failure' ? r.data : undefined;
  }

  const lookupSubmit: SubmitFunction = () => {
    return async ({ result }) => {
      const p = payloadOf(result);
      const lk = p?.lookup;
      if (!lk || lk.error) {
        // Selection stays; the probe failed and says so.
        tokenPresent = null;
        subState = { state: 'unknown', error: lk?.error ?? 'lookup failed', checkedAt: null };
        return;
      }
      if (lk.user) reconcileUser(lk.user);
      tokenPresent = lk.tokenPresent ?? null;
      subState = lk.subState ?? null;
    };
  };

  // Merge a server-echoed user row into the list (and detached selection).
  function reconcileUser(u: AdminUserWire) {
    if (!dir) return;
    const i = dir.recent.findIndex((r) => r.id === u.id);
    if (i >= 0) dir.recent[i] = u;
    else if (selectedId === String(u.id)) detached = u;
  }

  // ── Optimistic mutation plumbing ───────────────────────────────────────────
  // Apply the expected result instantly, keep a snapshot, then reconcile with
  // the echoed row on success or roll back + toast the real error on failure.
  let busyVerb = $state<string | null>(null);

  function mutate(verb: string, optimistic?: (u: AdminUserWire) => AdminUserWire): SubmitFunction {
    return () => {
      busyVerb = verb;
      const id = selectedId;
      const before = dir ? dir.recent.map((r) => ({ ...r })) : [];
      const beforeDetached = detached ? { ...detached } : null;
      if (optimistic && id && dir) {
        const i = dir.recent.findIndex((r) => String(r.id) === id);
        if (i >= 0) dir.recent[i] = optimistic(dir.recent[i]);
        if (detached) detached = optimistic(detached);
      }
      return async ({ result }) => {
        busyVerb = null;
        const p = payloadOf(result);
        if (result.type === 'success' && p?.action?.ok) {
          toast('ok', p.action.notice);
          if (p.lookup?.user) reconcileUser(p.lookup.user);
          if (p.subState) subState = p.subState;
          if (p.viewAsUrl) viewAsUrl = p.viewAsUrl;
          return;
        }
        // Roll back the optimistic apply — the UI must not keep a state the
        // server refused.
        if (dir) dir.recent = before;
        detached = beforeDetached;
        toast('err', p?.action?.notice ?? p?.error ?? `${verb} failed`);
      };
    };
  }

  const setActiveSubmit = mutate('set active', (u) => ({ ...u, is_active: !u.is_active }));
  const banSubmit = mutate('ban', (u) => ({ ...u, banned: true }));
  const unbanSubmit = mutate('unban', (u) => ({ ...u, banned: false }));
  const statusSubmit = (status: string) => mutate('set status', (u) => ({ ...u, status }));
  const creatorSubmit = mutate('creator code');
  const impersonateSubmit = mutate('view as');

  const restartSubmit: SubmitFunction = () => {
    busyVerb = 'restart';
    subState = null; // honest: state unknown while the reconnect queues
    return async ({ result }) => {
      busyVerb = null;
      const p = payloadOf(result);
      if (result.type === 'success' && p?.action?.ok) {
        toast('ok', p.action.notice);
        subState = p.subState ?? null;
        return;
      }
      toast('err', p?.action?.notice ?? p?.error ?? 'restart failed');
    };
  };

  // Token wipes are destructive: no optimistic flip, the badge only turns off
  // once the server confirms.
  function tokenWipe(verb: string): SubmitFunction {
    return () => {
      busyVerb = verb;
      return async ({ result }) => {
        busyVerb = null;
        const p = payloadOf(result);
        if (result.type === 'success' && p?.action?.ok) {
          toast('ok', p.action.notice);
          tokenPresent = false;
          if (p.lookup?.user) reconcileUser(p.lookup.user);
          return;
        }
        toast('err', p?.action?.notice ?? p?.error ?? `${verb} failed`);
      };
    };
  }
  const resetSubmit = tokenWipe('reset');
  const clearTokenSubmit = tokenWipe('clear token');

  // ── Status change (paid needs a grant end date) ────────────────────────────
  let statusForms = $state<Record<string, HTMLFormElement | null>>({});
  let grantOpen = $state(false);
  let grantDate = $state('');
  let grantForm = $state<HTMLFormElement | null>(null);

  function requestStatus(status: string) {
    if (!selected || selected.status === status) return;
    if (status === 'paid') {
      grantDate = new Date(Date.now() + 30 * 864e5).toISOString().slice(0, 10);
      grantOpen = true;
      return;
    }
    statusForms[status]?.requestSubmit();
  }

  const grantSubmit = statusSubmit('paid');

  // ── Delete ─────────────────────────────────────────────────────────────────
  let deleteOpen = $state(false);
  let deleteForm = $state<HTMLFormElement | null>(null);
  const deleteSubmit: SubmitFunction = () => {
    busyVerb = 'delete';
    return async ({ result }) => {
      busyVerb = null;
      deleteOpen = false;
      const p = payloadOf(result);
      if (result.type === 'success' && p?.action?.ok) {
        toast('ok', p.action.notice);
        if (dir && selectedId) dir.recent = dir.recent.filter((r) => String(r.id) !== selectedId);
        closeInspector();
        return;
      }
      toast('err', p?.action?.notice ?? p?.error ?? 'delete failed');
    };
  };

  // ── Search + pagination (server-driven) ────────────────────────────────────
  // svelte-ignore state_referenced_locally
  let search = $state(data.search);
  $effect(() => {
    search = data.search;
  });
  function submitSearch() {
    const q = search.trim();
    goto(q ? `/users?q=${encodeURIComponent(q)}` : '/users', { keepFocus: true });
  }
  function pageHref(p: number): string {
    const params = new URLSearchParams();
    if (data.search) params.set('q', data.search);
    if (data.state) params.set('state', data.state);
    if (p > 1) params.set('page', String(p));
    const qs = params.toString();
    return qs ? `/users?${qs}` : '/users';
  }

  async function copyViewAs() {
    try {
      await navigator.clipboard.writeText(viewAsUrl);
      viewAsCopied = true;
      setTimeout(() => (viewAsCopied = false), 1500);
    } catch {
      viewAsCopied = false;
    }
  }

  function ago(iso?: string): string {
    if (!iso) return '—';
    const mins = Math.max(Math.round((Date.now() - new Date(iso).getTime()) / 60e3), 0);
    if (mins < 1) return 'now';
    if (mins < 60) return `${mins}m ago`;
    const hours = Math.round(mins / 60);
    if (hours < 48) return `${hours}h ago`;
    return `${Math.round(hours / 24)}d ago`;
  }

  function fmtDate(iso?: string): string {
    if (!iso) return 'unknown';
    return new Date(iso).toLocaleDateString(undefined, {
      year: 'numeric',
      month: 'short',
      day: 'numeric'
    });
  }

  const subTone = $derived.by(() => {
    switch (subState?.state) {
      case 'ok':
        return 'green';
      case 'failing':
      case 'revoked':
        return 'err';
      default:
        return 'warn';
    }
  });

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && selectedId) closeInspector();
  }
</script>

<section class="screen active">
  <PageHead eyebrow="Accounts" description="Search, inspect, and manage every registered broadcaster.">
    User <em>directory</em>
  </PageHead>

  <PageToolbar>
    {#snippet lead()}
      {#if dir}
        <div class="dir-stats">
          <span><b>{dir.stats.total_users.toLocaleString()}</b> total</span>
          <span class="mid">·</span>
          <span><b>{dir.stats.active_users.toLocaleString()}</b> active</span>
          <span class="mid">·</span>
          <span><b>{dir.stats.premium_users.toLocaleString()}</b> premium</span>
        </div>
      {:else}
        <Skeleton variant="pill" width="220px" />
      {/if}
    {/snippet}
    {#snippet trail()}
      <form
        class="search-form"
        onsubmit={(e) => {
          e.preventDefault();
          submitSearch();
        }}
      >
        <SearchInput bind:value={search} placeholder="Username or numeric id…" />
        <Button variant="ghost" type="submit">Search</Button>
      </form>
    {/snippet}
  </PageToolbar>

  {#if dir?.degraded}
    <AlertBanner>User directory is unreachable right now; nothing below is live.</AlertBanner>
  {/if}

  <div class="filter-row">
    <div class="seg" role="radiogroup" aria-label="User state">
      {#each STATES as st (st)}
        <button
          type="button"
          class="chip"
          class:on={stateFilter === st}
          role="radio"
          aria-checked={stateFilter === st}
          onclick={() => applyState(st)}
        >
          {st}
        </button>
      {/each}
    </div>
    <div class="filter-trail">
      <Button variant="ghost" onclick={exportCsv} disabled={visible.length === 0}>
        <Icon name="audit" size={13} /> Export CSV
      </Button>
    </div>
  </div>

  <div class="deck">
    <DeckList>
      {#if dir === null}
        <div class="row-skeletons">
          {#each [0, 1, 2, 3, 4, 5] as i (i)}<Skeleton variant="block" height="52px" />{/each}
        </div>
      {:else if visible.length}
        <div class="user-head" aria-hidden="true">
          <span></span>
          {#each [['user', 'user'], ['id', 'id'], ['tier', 'tier'], ['flags', ''], ['joined', 'joined'], ['updated', 'updated']] as [label, key] (label)}
            {#if key}
              <button type="button" class="sort-btn" class:on={sortKey === key} onclick={() => toggleSort(key as SortKey)}>
                {label}{sortKey === key ? (sortDir === 1 ? ' ↑' : ' ↓') : ''}
              </button>
            {:else}
              <span class="head-label">{label}</span>
            {/if}
          {/each}
        </div>
        <ul class="list" aria-label="Users">
          {#each visible as u (u.id)}
            <li>
              <!-- data-cursor="off": a row is a reading surface, not a control;
                   the custom cursor must not morph into a row-sized box. -->
              <button
                type="button"
                class="user-row"
                data-cursor="off"
                class:on={selectedId === String(u.id)}
                onclick={() => openUser(u)}
              >
                <span class="udot state-{stateOf(u)}"></span>
                <span class="uname">{u.username}</span>
                <span class="uid">#{u.id}</span>
                <span class="ucell"><span class="tag st-{u.status}">{u.status}</span></span>
                <span class="utags">
                  {#if u.banned}<span class="tag st-banned">banned</span>{/if}
                  {#if !u.is_active}<span class="tag st-inactive">inactive</span>{/if}
                  {#if u.creator_code}<span class="tag code">code</span>{/if}
                </span>
                <span class="uwhen">{ago(u.created_at)}</span>
                <span class="uwhen">{ago(u.updated_at)}</span>
              </button>
            </li>
          {/each}
        </ul>
      {:else if rows.length}
        <EmptyState icon="search" title="No users in this state on this page" />
      {:else if data.search}
        <EmptyState icon="search" title="No users match" body="Try the exact login or the numeric Twitch id." />
      {:else}
        <EmptyState icon="users" title="No users yet" />
      {/if}

      {#if dir && (dir.page > 1 || dir.hasMore)}
        <div class="pager">
          <a class="btn ghost" class:disabled={dir.page <= 1} href={pageHref(dir.page - 1)} aria-disabled={dir.page <= 1}>
            ← Prev
          </a>
          <span class="pager-label">page {dir.page} / {dir.maxPages}</span>
          <a class="btn ghost" class:disabled={!dir.hasMore} href={pageHref(dir.page + 1)} aria-disabled={!dir.hasMore}>
            Next →
          </a>
        </div>
      {/if}
    </DeckList>

    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="inspector-backdrop"
      class:open={selected !== null}
      role="presentation"
      onclick={closeInspector}
      onkeydown={(e) => {
        if (e.key === 'Enter') closeInspector();
      }}
    ></div>
    <aside class="inspector" class:open={selected !== null} aria-label="User inspector">
      <div class="inspector-head">
        <span class="inspector-tag">{selected ? `@${selected.username}` : 'Inspector'}</span>
        {#if selected}
          <button class="mini" type="button" aria-label="Close" onclick={closeInspector}>
            <Icon name="x" size={14} />
          </button>
        {/if}
      </div>

      {#if selected}
        <Scroller fill padding="18px" data-lenis-prevent>
          <div class="detail">
            <div class="ident">
              <span class="avatar">{selected.username.slice(0, 1).toUpperCase()}</span>
              <div>
                <div class="ident-name">{selected.username}</div>
                <div class="ident-meta">
                  #{selected.id} · joined {fmtDate(selected.created_at)} · updated {ago(selected.updated_at)}
                </div>
              </div>
            </div>

            <div class="block">
              <span class="block-label">Tier</span>
              <div class="chips">
                {#each ['free', 'paid', 'vip'] as st (st)}
                  <button
                    type="button"
                    class="chip chip-{st}"
                    class:on={selected.status === st}
                    disabled={busyVerb !== null}
                    onclick={() => requestStatus(st)}
                  >
                    {st}
                  </button>
                {/each}
              </div>
              {#if selected.subscription_expires_at || selected.subscription_source || selected.subscription_ref}
                <dl class="subfacts">
                  {#if selected.subscription_expires_at}
                    <div><dt>Runs until</dt><dd>{fmtDate(selected.subscription_expires_at)}</dd></div>
                  {/if}
                  {#if selected.subscription_source}
                    <div><dt>Source</dt><dd>{selected.subscription_source}</dd></div>
                  {/if}
                  {#if selected.subscription_ref}
                    <div><dt>Reference</dt><dd>{selected.subscription_ref}</dd></div>
                  {/if}
                  {#if selected.subscription_cancel_pending}
                    <div><dt>Renewal</dt><dd class="cancel">cancel pending</dd></div>
                  {/if}
                </dl>
              {/if}
            </div>

            <div class="block">
              <span class="block-label">Service</span>
              <div class="btn-row">
                <form method="POST" action="?/setActive" use:enhance={setActiveSubmit}>
                  <input type="hidden" name="user_id" value={selected.id} />
                  <input type="hidden" name="active" value={selected.is_active ? 'false' : 'true'} />
                  <button class="btn ghost" type="submit" disabled={busyVerb !== null}>
                    {selected.is_active ? 'Deactivate' : 'Activate'}
                  </button>
                </form>
                {#if selected.banned}
                  <form method="POST" action="?/unban" use:enhance={unbanSubmit}>
                    <input type="hidden" name="user_id" value={selected.id} />
                    <button class="btn ghost" type="submit" disabled={busyVerb !== null}>Unban</button>
                  </form>
                {:else}
                  <form method="POST" action="?/ban" use:enhance={banSubmit}>
                    <input type="hidden" name="user_id" value={selected.id} />
                    <button class="btn ghost danger" type="submit" disabled={busyVerb !== null}>Ban</button>
                  </form>
                {/if}
              </div>
            </div>

            <!-- Bot health: token + EventSub, probed live on select -->
            <div class="block">
              <span class="block-label">Bot health</span>
              <div class="probe-row">
                <span class="probe-dot {tokenPresent === null ? 'warn' : tokenPresent ? 'green' : 'err'}"></span>
                <span>{tokenPresent === null ? 'Checking token…' : tokenPresent ? 'OAuth token stored' : 'No token stored'}</span>
              </div>
              <div class="probe-row">
                <span class="probe-dot {subState === null ? 'warn' : subTone}"></span>
                <span>
                  {#if subState === null}
                    Checking EventSub…
                  {:else}
                    EventSub {subState.state}{subState.checkedAt ? ` · checked ${ago(subState.checkedAt)}` : ''}
                  {/if}
                </span>
              </div>
              {#if subState?.error}
                <span class="probe-err">{subState.error}</span>
              {/if}
              <div class="btn-row">
                <form method="POST" action="?/restart" use:enhance={restartSubmit}>
                  <input type="hidden" name="user_id" value={selected.id} />
                  <button class="btn ghost" type="submit" disabled={busyVerb !== null}>
                    {busyVerb === 'restart' ? 'Restarting…' : 'Restart bot'}
                  </button>
                </form>
              </div>
            </div>

            <div class="block">
              <span class="block-label">Creator code</span>
              <form class="creator" method="POST" action="?/setCreatorCode" use:enhance={creatorSubmit}>
                <input type="hidden" name="user_id" value={selected.id} />
                <input
                  class="text-input"
                  type="text"
                  name="creator_code"
                  maxlength="64"
                  placeholder="none"
                  value={selected.creator_code ?? ''}
                />
                <button class="btn ghost" type="submit" disabled={busyVerb !== null}>Save</button>
              </form>
            </div>

            <div class="block">
              <span class="block-label">Support</span>
              <div class="btn-row">
                <form method="POST" action="?/impersonate" use:enhance={impersonateSubmit}>
                  <input type="hidden" name="user_id" value={selected.id} />
                  <button class="btn ghost" type="submit" disabled={busyVerb !== null}>
                    <Icon name="link" size={13} /> Mint view-as link
                  </button>
                </form>
                <button class="btn ghost" type="button" disabled={busyVerb !== null} onclick={openMessage}>
                  <Icon name="send" size={13} /> Message user
                </button>
              </div>
              {#if viewAsUrl}
                <div class="viewas">
                  <input class="text-input" type="text" readonly value={viewAsUrl} />
                  <button class="btn ghost" type="button" onclick={copyViewAs}>
                    {viewAsCopied ? 'Copied' : 'Copy'}
                  </button>
                </div>
                <span class="block-note">valid 5 minutes; actions are attributed to you</span>
              {/if}
            </div>

            {#if isManager}
              <div class="block">
                <span class="block-label">Operator actions on this user</span>
                {#if targetHistory === null}
                  <span class="block-note">Loading history…</span>
                {:else if targetHistoryError}
                  <span class="probe-err">{targetHistoryError}</span>
                {:else if targetHistory.length === 0}
                  <span class="block-note">None recorded.</span>
                {:else}
                  <ul class="thist">
                    {#each targetHistory as e (e.id)}
                      <li class="thist-row">
                        <span class="probe-dot {e.ok ? 'green' : 'err'}"></span>
                        <span class="thist-act">{e.action} · @{e.actor_login}</span>
                        <span class="thist-when">{ago(e.created_at)}</span>
                      </li>
                    {/each}
                  </ul>
                  <a class="block-note thist-more" href={`/audit?q=${selected.id}`}>Full history in audit →</a>
                {/if}
              </div>
            {/if}

            <div class="block danger-zone">
              <span class="block-label">Danger</span>
              <div class="btn-row">
                <form method="POST" action="?/reset" use:enhance={resetSubmit}>
                  <input type="hidden" name="user_id" value={selected.id} />
                  <button class="btn ghost" type="submit" disabled={busyVerb !== null}>Reset tokens</button>
                </form>
                <form method="POST" action="?/clearToken" use:enhance={clearTokenSubmit}>
                  <input type="hidden" name="user_id" value={selected.id} />
                  <button class="btn ghost" type="submit" disabled={busyVerb !== null}>Clear token</button>
                </form>
                <button
                  class="btn ghost danger"
                  type="button"
                  disabled={busyVerb !== null}
                  onclick={() => (deleteOpen = true)}
                >
                  Delete user
                </button>
              </div>
            </div>
          </div>
        </Scroller>
      {:else}
        <div class="inspector-idle">
          <span class="idle-glyph"><Icon name="users" size={18} /></span>
          <p>Select a user to inspect their account, bot health, and tier.</p>
        </div>
      {/if}
    </aside>
  </div>
</section>

<svelte:window onkeydown={onKey} />

<!-- Hidden lookup probe: fired on row select to fetch token + EventSub state. -->
<form method="POST" action="?/lookup" use:enhance={lookupSubmit} bind:this={lookupForm} hidden>
  <input type="hidden" name="q" value={lookupQ} />
</form>

<!-- Hidden status forms (free/vip immediate; paid goes through the grant modal). -->
{#each ['free', 'vip'] as st (st)}
  <form
    method="POST"
    action="?/setStatus"
    use:enhance={statusSubmit(st)}
    bind:this={statusForms[st]}
    hidden
  >
    <input type="hidden" name="user_id" value={selected?.id ?? ''} />
    <input type="hidden" name="status" value={st} />
  </form>
{/each}

<form method="POST" action="?/setStatus" use:enhance={grantSubmit} bind:this={grantForm} hidden>
  <input type="hidden" name="user_id" value={selected?.id ?? ''} />
  <input type="hidden" name="status" value="paid" />
  <input type="hidden" name="expires_at" value={grantDate} />
</form>

<ConfirmDialog
  open={grantOpen}
  title="Grant paid tier"
  body={selected
    ? `Every paid grant carries the day it ends. @${selected.username} runs premium until end-of-day on the chosen date.`
    : undefined}
  confirmLabel="Grant paid"
  cancelLabel="Cancel"
  busy={busyVerb === 'set status'}
  onCancel={() => (grantOpen = false)}
  onConfirm={() => {
    grantOpen = false;
    grantForm?.requestSubmit();
  }}
>
  <label class="grant-label">
    Ends on
    <input
      class="text-input"
      type="date"
      bind:value={grantDate}
      min={new Date(Date.now() + 864e5).toISOString().slice(0, 10)}
    />
  </label>
</ConfirmDialog>

<!-- Direct notification: posts to the notifications route's send action so
     delivery, audit, and idempotency stay in one place. -->
<ConfirmDialog
  open={msgOpen}
  title={selected ? `Message @${selected.username}` : 'Message user'}
  body="Lands in their dashboard notification bell."
  confirmLabel="Send"
  cancelLabel="Cancel"
  busy={busyVerb === 'message'}
  onCancel={() => (msgOpen = false)}
  onConfirm={() => msgForm?.requestSubmit()}
>
  <div class="msg-fields">
    <label>Title
      <input class="text-input" type="text" maxlength="120" bind:value={msgTitle} />
    </label>
    <label>Message
      <textarea class="text-input" rows="3" maxlength="2000" bind:value={msgBody}></textarea>
    </label>
    <label>Level
      <select class="text-input" bind:value={msgLevel}>
        <option value="info">Info</option>
        <option value="success">Success</option>
        <option value="warning">Warning</option>
        <option value="critical">Critical</option>
      </select>
    </label>
  </div>
</ConfirmDialog>
<form method="POST" action="/notifications?/send" use:enhance={msgSubmit} bind:this={msgForm} hidden>
  <input type="hidden" name="scope" value="direct" />
  <input type="hidden" name="target_user_id" value={selected?.id ?? ''} />
  <input type="hidden" name="target_username" value="" />
  <input type="hidden" name="title" value={msgTitle} />
  <input type="hidden" name="body" value={msgBody} />
  <input type="hidden" name="level" value={msgLevel} />
  <input type="hidden" name="expires_at" value="" />
</form>

<ConfirmDialog
  open={deleteOpen}
  title="Delete user"
  body={selected
    ? `This permanently removes @${selected.username} (#${selected.id}) and their stored tokens from the users service.`
    : undefined}
  confirmLabel="Delete"
  cancelLabel="Cancel"
  danger
  busy={busyVerb === 'delete'}
  onCancel={() => (deleteOpen = false)}
  onConfirm={() => deleteForm?.requestSubmit()}
/>
<form method="POST" action="?/delete" use:enhance={deleteSubmit} bind:this={deleteForm} hidden>
  <input type="hidden" name="user_id" value={selected?.id ?? ''} />
</form>

<style>
  .dir-stats {
    display: flex; gap: 8px; align-items: baseline; flex-wrap: wrap;
    font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted);
  }
  .dir-stats b { color: var(--bb-white); font-weight: 600; }
  .dir-stats .mid { color: var(--rule-strong); }
  .search-form { display: flex; gap: 8px; align-items: center; }

  .row-skeletons { display: flex; flex-direction: column; gap: 8px; padding: 12px; }

  .deck { display: grid; grid-template-columns: minmax(0, 1fr); gap: 16px; align-items: start; }
  @media (min-width: 1080px) {
    .deck { grid-template-columns: minmax(0, 1fr) 340px; }
  }

  .filter-row {
    display: flex; align-items: center; justify-content: space-between; gap: 12px;
    flex-wrap: wrap; margin-bottom: 14px;
  }
  .filter-trail { display: flex; align-items: center; gap: 12px; }
  .seg { display: flex; gap: 6px; flex-wrap: wrap; }

  .thist { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 7px; }
  .thist-row { display: flex; align-items: center; gap: 9px; min-width: 0; }
  .thist-act {
    font-family: var(--bb-font-mono); font-size: 11.5px; color: var(--bb-white);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1;
  }
  .thist-when { font-family: var(--bb-font-mono); font-size: 10px; color: var(--bb-muted); white-space: nowrap; }
  .thist-more { text-decoration: none; color: var(--bb-tan); }
  .thist-more:hover { color: var(--bb-tan-pale); }

  .msg-fields { display: flex; flex-direction: column; gap: 12px; margin: 12px 0 4px; }
  .msg-fields label {
    display: flex; flex-direction: column; gap: 6px;
    font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted);
  }
  .msg-fields textarea { resize: vertical; font-family: var(--bb-font-body); }

  /* ── Five state colors. VIP is silver, deliberately not purple. ─────────── */
  .list { list-style: none; margin: 0; padding: 0; }

  /* Header and rows share one grid template so every column lines up. */
  .user-head, .user-row {
    display: grid;
    grid-template-columns: 14px minmax(130px, 1.3fr) 100px 72px minmax(0, 1fr) 82px 82px;
    align-items: center;
    gap: 12px;
    width: 100%;
    padding: 13px 14px;
  }
  .user-head {
    padding-top: 10px; padding-bottom: 8px;
    border-bottom: 1px solid var(--rule-strong);
  }
  .sort-btn, .head-label {
    font-family: var(--bb-font-mono); font-size: 10px; letter-spacing: 0.12em;
    text-transform: uppercase; color: var(--bb-muted); text-align: left;
    background: none; border: none; padding: 0;
  }
  .sort-btn { cursor: pointer; }
  .sort-btn:hover { color: var(--bb-white); }
  .sort-btn.on { color: var(--bb-tan-light); }

  .user-row {
    background: none;
    border: none;
    border-bottom: 1px solid var(--rule);
    color: inherit;
    cursor: pointer;
    text-align: left;
    transition: background 120ms ease;
  }
  li:last-child .user-row { border-bottom: none; }
  .user-row:hover { background: rgba(240, 236, 228, 0.03); }
  .user-row.on { background: rgba(201, 168, 124, 0.06); }

  .udot { width: 8px; height: 8px; border-radius: 50%; flex: none; }
  .udot.state-free { background: var(--bb-green-glow); box-shadow: 0 0 8px var(--bb-green-glow); }
  .udot.state-paid { background: var(--bb-tan-light); box-shadow: 0 0 8px rgba(224, 196, 154, 0.6); }
  .udot.state-vip { background: #d9dee4; box-shadow: 0 0 8px rgba(217, 222, 228, 0.55); }
  .udot.state-banned { background: #cf8a78; box-shadow: 0 0 8px rgba(176, 90, 70, 0.6); }
  .udot.state-inactive { background: #8fa8bf; box-shadow: 0 0 8px rgba(143, 168, 191, 0.5); }

  .uname { font-family: var(--bb-font-body); font-weight: 600; font-size: 13.5px; color: var(--bb-white); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .uid { font-family: var(--bb-font-mono); font-size: 11.5px; color: var(--bb-muted); white-space: nowrap; }
  .ucell { display: flex; }
  .utags { display: flex; gap: 6px; flex-wrap: wrap; }
  .tag {
    font-family: var(--bb-font-mono); font-size: 10px; letter-spacing: 0.08em; text-transform: uppercase;
    padding: 2px 8px; border-radius: var(--bb-radius-pill); border: 1px solid transparent; white-space: nowrap;
  }
  .tag.st-free { color: var(--bb-green-glow); background: rgba(82, 183, 136, 0.1); border-color: rgba(82, 183, 136, 0.28); }
  .tag.st-paid { color: var(--bb-tan-light); background: rgba(201, 168, 124, 0.1); border-color: rgba(201, 168, 124, 0.3); }
  .tag.st-vip { color: #dfe4e9; background: rgba(217, 222, 228, 0.1); border-color: rgba(217, 222, 228, 0.34); }
  .tag.st-banned { color: #cf8a78; background: rgba(176, 90, 70, 0.1); border-color: rgba(176, 90, 70, 0.32); }
  .tag.st-inactive { color: #a7bccd; background: rgba(143, 168, 191, 0.1); border-color: rgba(143, 168, 191, 0.3); }
  .tag.code { color: var(--bb-muted); background: rgba(255, 255, 255, 0.03); border-color: var(--glass-border); }
  .uwhen { font-family: var(--bb-font-mono); font-size: 11px; color: var(--bb-muted); white-space: nowrap; }

  .pager { display: flex; align-items: center; justify-content: center; gap: 14px; padding: 14px; }
  .pager-label { font-family: var(--bb-font-mono); font-size: 11.5px; color: var(--bb-muted); }
  .pager .btn.disabled { opacity: 0.35; pointer-events: none; }

  /* ── Inspector ─────────────────────────────────────────────────────────── */
  .inspector {
    position: sticky;
    top: 62px;
    border: 1px solid var(--rule);
    border-top-color: var(--rule-strong);
    border-radius: 8px;
    background: linear-gradient(180deg, rgba(240, 236, 228, 0.03), rgba(240, 236, 228, 0.012));
    display: flex;
    flex-direction: column;
    max-height: calc(100vh - 62px - 108px);
  }
  .inspector-head {
    display: flex; align-items: center; justify-content: space-between; gap: 10px;
    padding: 12px 16px; border-bottom: 1px solid var(--rule);
  }
  .inspector-tag {
    font-family: var(--bb-font-display); font-weight: 700; font-size: 12px;
    letter-spacing: 0.02em; color: var(--bb-tan);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .inspector-idle {
    padding: 34px 20px; text-align: center; color: var(--bb-muted);
    font-family: var(--bb-font-body); font-size: 13px;
    display: flex; flex-direction: column; align-items: center; gap: 12px;
  }
  .idle-glyph {
    display: inline-flex; align-items: center; justify-content: center;
    width: 40px; height: 40px; border: 1px solid var(--rule-tan); border-radius: 8px;
    color: var(--bb-tan-light);
  }
  .inspector-idle p { margin: 0; max-width: 26ch; line-height: 1.5; }

  .detail { display: flex; flex-direction: column; gap: 20px; }
  .ident { display: flex; align-items: center; gap: 12px; }
  .avatar {
    width: 42px; height: 42px; border-radius: 50%; flex: none;
    display: inline-flex; align-items: center; justify-content: center;
    font-family: var(--bb-font-display); font-weight: 800; font-size: 17px;
    color: var(--bb-tan-light);
    background: rgba(201, 168, 124, 0.1); border: 1px solid rgba(201, 168, 124, 0.3);
  }
  .ident-name { font-family: var(--bb-font-display); font-weight: 700; font-size: 16px; color: var(--bb-white); }
  .ident-meta { font-family: var(--bb-font-mono); font-size: 11.5px; color: var(--bb-muted); margin-top: 2px; }

  .block { display: flex; flex-direction: column; gap: 9px; }
  .block-label {
    font-family: var(--bb-font-mono); font-size: 10px; letter-spacing: 0.12em;
    text-transform: uppercase; color: var(--bb-muted);
  }
  .block-note { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }

  .chips { display: flex; gap: 6px; flex-wrap: wrap; }
  .chip {
    font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.06em;
    padding: 7px 14px; border-radius: var(--bb-radius-pill); white-space: nowrap;
    background: rgba(255, 255, 255, 0.03); border: 1px solid var(--glass-border);
    color: var(--bb-muted); cursor: pointer;
  }
  .chip:hover:not(:disabled) { color: var(--bb-white); border-color: var(--bb-border-strong); }
  .chip.on { color: var(--bb-white); background: var(--ui-accent-soft); border-color: var(--bb-border-strong); }
  .chip:disabled { opacity: 0.5; cursor: not-allowed; }
  /* Active tier chip wears the tier's state color (VIP silver, not purple). */
  .chip-free.on { color: var(--bb-green-glow); background: rgba(82, 183, 136, 0.12); border-color: rgba(82, 183, 136, 0.35); }
  .chip-paid.on { color: var(--bb-tan-light); background: rgba(201, 168, 124, 0.12); border-color: rgba(201, 168, 124, 0.38); }
  .chip-vip.on { color: #dfe4e9; background: rgba(217, 222, 228, 0.12); border-color: rgba(217, 222, 228, 0.4); }

  .subfacts { display: flex; flex-direction: column; gap: 7px; margin: 2px 0 0; }
  .subfacts div { display: flex; justify-content: space-between; gap: 12px; align-items: baseline; }
  .subfacts dt { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); }
  .subfacts dd {
    margin: 0; font-family: var(--bb-font-mono); font-size: 11.5px; color: var(--bb-tan-light);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .subfacts dd.cancel { color: #cf8a78; }

  .btn-row { display: flex; gap: 8px; flex-wrap: wrap; }
  .btn.danger { color: #cf8a78; border-color: rgba(176, 90, 70, 0.4); }
  .btn.danger:hover { background: rgba(176, 90, 70, 0.12); color: #e0a293; }

  .probe-row {
    display: flex; align-items: center; gap: 9px;
    font-family: var(--bb-font-body); font-size: 13px; color: var(--bb-white);
  }
  .probe-dot { width: 8px; height: 8px; border-radius: 50%; flex: none; }
  .probe-dot.green { background: var(--bb-green-glow); box-shadow: 0 0 8px var(--bb-green-glow); }
  .probe-dot.warn { background: var(--bb-tan); box-shadow: 0 0 8px var(--bb-tan); }
  .probe-dot.err { background: #cf8a78; box-shadow: 0 0 8px rgba(176, 90, 70, 0.6); }
  .probe-err {
    font-family: var(--bb-font-mono); font-size: 11px; color: #cf8a78;
    word-break: break-word;
  }

  .creator { display: flex; gap: 8px; }
  .text-input {
    flex: 1; min-width: 0; padding: 8px 11px;
    font-family: var(--bb-font-mono); font-size: 12.5px;
    border: 1px solid var(--rule); border-radius: 8px;
    background: var(--bb-bg-1, #16130f); color: var(--bb-white);
  }
  .text-input:focus { outline: none; border-color: var(--bb-border-strong); }

  .viewas { display: flex; gap: 8px; }

  .danger-zone { padding-top: 14px; border-top: 1px solid var(--rule); }

  .grant-label {
    display: flex; flex-direction: column; gap: 8px; margin: 12px 0 4px;
    font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted);
  }

  .inspector-backdrop { display: none; }
  @media (max-width: 1079px) {
    .inspector { display: none; }
    .inspector.open {
      display: flex;
      position: fixed;
      left: 0; right: 0; bottom: 0; top: auto;
      z-index: 220;
      max-height: 88vh;
      border-radius: 8px 8px 0 0;
      background: var(--bb-bg-1, #111);
      animation: sheet-in var(--bb-dur-base, 320ms) var(--bb-ease-out-expo, cubic-bezier(0.16, 1, 0.3, 1)) both;
    }
    .inspector-backdrop.open {
      display: block; position: fixed; inset: 0; z-index: 219;
      background: rgba(0, 0, 0, 0.55);
    }
    @keyframes sheet-in {
      from { transform: translateY(100%); }
      to { transform: translateY(0); }
    }
  }

  @media (max-width: 900px) {
    .user-head { display: none; }
    .user-row { grid-template-columns: 14px minmax(0, 1fr) auto auto; }
    .uid, .utags, .uwhen:nth-last-child(2) { display: none; }
  }
  @media (max-width: 680px) {
    .search-form { width: 100%; }
    .search-form :global(.search) { flex: 1; }
    .filter-trail { width: 100%; justify-content: space-between; }
  }
</style>
