<script lang="ts">
  import { enhance } from '$app/forms';
  import { untrack } from 'svelte';
  import { Icon, StatTile, Button, Modal, Drawer } from '@bagel/shared';
  import type { AdminUserWire } from '$lib/server/rpc';
  let { data, form } = $props();

  const lookup = $derived(form?.lookup as Record<string, unknown> | undefined);
  const found = $derived(lookup?.user as AdminUserWire | undefined);
  const lookupError = $derived(lookup?.error as string | undefined);
  const tokenPresent = $derived(lookup?.tokenPresent === undefined ? undefined : Boolean(lookup?.tokenPresent));
  const action = $derived(form?.action as { ok: boolean; notice: string } | undefined);
  const viewAsUrl = $derived(form?.viewAsUrl as string | undefined);
  const search = $derived(String(data.search ?? ''));
  const page = $derived(Number(data.page ?? 1));
  const pageSize = $derived(Number(data.pageSize ?? 15));
  const maxPages = $derived(Number(data.maxPages ?? 25));
  const hasMore = $derived(Boolean(data.hasMore));

  // Copy the freshly-minted view-as link to the clipboard (mirrors the bot-link
  // copy on the overview page).
  let copied = $state(false);
  async function copyViewAs() {
    if (!viewAsUrl) return;
    try {
      await navigator.clipboard.writeText(viewAsUrl);
      copied = true;
      setTimeout(() => (copied = false), 1500);
    } catch {
      copied = false;
    }
  }

  function tier(status: string): 'premium' | 'standard' {
    return status === 'paid' || status === 'vip' ? 'premium' : 'standard';
  }

  function usersHref(pageNo: number, q: string): string {
    const params = new URLSearchParams();
    const clean = q.trim();
    if (clean) params.set('q', clean);
    if (pageNo > 1) params.set('page', String(pageNo));
    const query = params.toString();
    return query ? `/users?${query}` : '/users';
  }

  // Apply the result without invalidateAll: mutations reply with the updated
  // user (authoritative), so we reconcile that one row locally instead of
  // re-running load. The old update()+invalidateAll both stalled the response
  // and, under concurrent submits, raced a full refetch against the writes.
  function refresh() {
    return async ({ update }: { update: (opts?: { invalidateAll?: boolean }) => Promise<void> }) => {
      await update({ invalidateAll: false });
    };
  }

  // Local copy of the recent list, seeded from SSR. Reconciled per row from the
  // freshest user the server returns, so badges/state update without a refetch.
  // svelte-ignore state_referenced_locally
  let recent = $state<AdminUserWire[]>(data.recent);
  const showingStart = $derived(recent.length === 0 ? 0 : (page - 1) * pageSize + 1);
  const showingEnd = $derived(showingStart === 0 ? 0 : showingStart + recent.length - 1);
  // svelte-ignore state_referenced_locally
  let seed = data.recent;
  $effect(() => {
    const nextSeed = data.recent;
    untrack(() => {
      if (nextSeed !== seed) {
        seed = nextSeed;
        recent = nextSeed;
      }
    });
  });

  function upsertRecent(u: AdminUserWire) {
    const i = recent.findIndex((r) => String(r.id) === String(u.id));
    if (i >= 0) recent[i] = u;
    // Not in the recent window: leave the list as-is (lookup may hit any user).
  }

  function removeRecent(id: number | string) {
    recent = recent.filter((r) => String(r.id) !== String(id));
  }

  // --- Selected user (drawer) state -----------------------------------
  let selected = $state<AdminUserWire | null>(null);

  // When a lookup or a mutation returns a user, sync it into the drawer and the
  // recent row so the displayed state always reflects the freshest server truth.
  $effect(() => {
    if (found) {
      untrack(() => {
        if (!selected || String(selected.id) !== String(found.id)) {
          selected = found;
        }
        upsertRecent(found);
      });
    }
  });

  // The user shown in the drawer. Prefer the form-returned user when its id
  // matches the selection (post-mutation truth), else the clicked row.
  const drawerUser = $derived.by<AdminUserWire | null>(() => {
    if (!selected) return null;
    if (found && String(found.id) === String(selected.id)) return found;
    return selected;
  });

  const drawerToken = $derived(
    found && drawerUser && String(found.id) === String(drawerUser.id) ? tokenPresent : undefined
  );

  function openUser(u: AdminUserWire) {
    selected = u;
  }
  function closeDrawer() {
    selected = null;
  }
  function handleRowKey(e: KeyboardEvent, u: AdminUserWire) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      openUser(u);
    }
  }

  // --- Confirm-delete modal state -------------------------------------
  let deleteTarget = $state<{ id: number | string; username: string } | null>(null);

  function openDelete(u: { id: number | string; username: string }) {
    deleteTarget = u;
  }
  function closeDelete() {
    deleteTarget = null;
  }

  // Esc closes modal first, then drawer.
  function handleKey(e: KeyboardEvent) {
    if (e.key !== 'Escape') return;
    if (deleteTarget) closeDelete();
    else if (selected) closeDrawer();
  }

  const rows = $derived(recent);

  const tiers: Array<{ key: string; label: string }> = [
    { key: 'free', label: 'Free' },
    { key: 'paid', label: 'Paid' },
    { key: 'vip', label: 'VIP' }
  ];
</script>

<svelte:window onkeydown={handleKey} />

<section class="screen active users-screen">
  <div class="page-head">
    <span class="eyebrow">Broadcaster accounts</span>
    <h1>User <em>management</em></h1>
    <p>
      Grants, resets, and paged user search.{#if data.degraded}
        <em> Live user data unavailable; showing sample.</em>{/if}
    </p>
  </div>

  <div class="stat-grid">
    <StatTile icon="users" label="Registered" value={data.stats.total_users.toLocaleString()} unit="total" delta={`${data.stats.active_users} active`} flat />
    <StatTile icon="pulse" tan label="Premium" value={data.stats.premium_users.toLocaleString()} unit="" delta="paid + vip" flat />
    <StatTile icon="heart" label="VIP" value={data.stats.vip_users.toLocaleString()} unit="" delta="comped" flat />
    <StatTile icon="commands" tan label="Paid" value={data.stats.paid_users.toLocaleString()} unit="" delta="subscribers" flat />
  </div>

  <!-- Prominent lookup bar -->
  <div class="card lookup-card">
    <form method="POST" action="?/lookup" use:enhance={refresh} class="lookup-form">
      <label class="search lookup-input">
        <Icon name="search" size={15} />
        <input name="q" type="text" placeholder="Look up a Twitch user id or username" autocomplete="off" />
      </label>
      <Button variant="primary" icon="search" type="submit">Look up</Button>
    </form>
    {#if lookupError}
      <p class="notice-muted">{lookupError}</p>
    {/if}
  </div>

  <!-- MASTER: calm recent list -->
  <div class="card recent-card">
    <div class="card-head recent-head">
      <h3>Users</h3>
      <form method="GET" action="/users" class="users-controls">
        <label class="search search-filter">
          <Icon name="search" size={14} />
          <input name="q" type="text" placeholder="Search by name or id" autocomplete="off" value={search} />
        </label>
        <button class="btn primary search-submit" type="submit">
          <Icon name="search" size={14} />
          <span>Search</span>
        </button>
        {#if search}
          <a class="btn ghost clear-search" href="/users">Clear</a>
        {/if}
      </form>
    </div>
    <div class="table users-table">
      <div class="thead">
        <span>User</span><span>Id</span><span class="perm-cell">Tier</span><span>Status</span><span>Active</span><span></span>
      </div>
      <div class="trows">
        {#if rows.length === 0}
          <div class="trow"><span class="resp" style="grid-column:1/-1;opacity:.6">No matching users.</span></div>
        {/if}
        {#each rows as u (u.id)}
          <div
            class="trow trow-clickable"
            class:selected={selected && String(selected.id) === String(u.id)}
            role="button"
            tabindex="0"
            onclick={() => openUser(u)}
            onkeydown={(e) => handleRowKey(e, u)}
          >
            <span class="cmd">@{u.username}{#if u.banned}<span class="badge banned">banned</span>{/if}</span>
            <span class="resp">{u.id}</span>
            <span class="perm-cell"><span class="badge {tier(u.status) === 'premium' ? 'sub' : 'everyone'}">{tier(u.status)}</span></span>
            <span class="cd">{u.status}</span>
            <span class="uses">{u.is_active ? 'yes' : 'no'}</span>
            <span class="row-act"><span class="chev" aria-hidden="true"></span></span>
          </div>
        {/each}
      </div>
    </div>

    <div class="users-foot">
      <span class="page-state">
        {#if showingStart === 0}
          Page {page}
        {:else}
          {showingStart}-{showingEnd} · page {page}
        {/if}
        <span class="muted">of {maxPages} max</span>
      </span>
      <div class="pager">
        {#if page > 1}
          <a class="pager-link" href={usersHref(page - 1, search)}>Previous</a>
        {:else}
          <span class="pager-link disabled" aria-disabled="true">Previous</span>
        {/if}
        {#if hasMore && page < maxPages}
          <a class="pager-link" href={usersHref(page + 1, search)}>Next</a>
        {:else}
          <span class="pager-link disabled" aria-disabled="true">Next</span>
        {/if}
      </div>
    </div>
  </div>
</section>

<!-- DETAIL DRAWER -->
{#if drawerUser}
  <!-- A full-screen <button> would be matched by the custom cursor's interactive
       selector and morph the tan ring over the entire page; use a div instead. -->
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div class="drawer-backdrop" role="button" tabindex="-1" aria-label="Close drawer" onclick={closeDrawer}></div>
  <div class="drawer open" role="dialog" aria-modal="true" aria-labelledby="drawer-title">
    <header class="drawer-head">
      <div class="drawer-id">
        <h2 id="drawer-title">@{drawerUser.username}</h2>
        <span class="drawer-sub">id {drawerUser.id} · {tier(drawerUser.status)}</span>
      </div>
      <button class="drawer-close" type="button" onclick={closeDrawer} aria-label="Close">
        <Icon name="x" size={16} />
      </button>
    </header>

    <div class="drawer-body" data-lenis-prevent>
      <!-- Profile meta -->
      <div class="meta-block">
        <div class="meta-line"><span class="meta-k">Status</span><span class="meta-v">{drawerUser.status}</span></div>
        <div class="meta-line"><span class="meta-k">State</span><span class="meta-v">{drawerUser.is_active ? 'active' : 'inactive'}</span></div>
        <div class="meta-line"><span class="meta-k">Ban</span><span class="meta-v">{drawerUser.banned ? 'banned' : 'allowed'}</span></div>
        <div class="meta-line">
          <span class="meta-k">Token</span>
          <span class="meta-v">{drawerToken === undefined ? 'unknown' : drawerToken ? 'present' : 'absent'}</span>
        </div>
        <div class="meta-line">
          <span class="meta-k">Tier</span>
          <span class="badge {tier(drawerUser.status) === 'premium' ? 'sub' : 'everyone'}">{tier(drawerUser.status)}</span>
        </div>
      </div>

      {#if action}
        <p class="notice-{action.ok ? 'ok' : 'err'}">{action.notice}</p>
      {/if}

      <!-- Tier segment -->
      <div class="field">
        <span class="field-label">Tier</span>
        <div class="segment">
          {#each tiers as t}
            <form method="POST" action="?/setStatus" use:enhance={refresh}>
              <input type="hidden" name="user_id" value={drawerUser.id} />
              <input type="hidden" name="status" value={t.key} />
              <button
                class="seg-btn"
                class:on={drawerUser.status === t.key}
                type="submit"
                disabled={drawerUser.status === t.key}
                aria-pressed={drawerUser.status === t.key}
              >{t.label}</button>
            </form>
          {/each}
        </div>
      </div>

      <!-- Active toggle -->
      <div class="field">
        <span class="field-label">Activation</span>
        <form method="POST" action="?/setActive" use:enhance={refresh}>
          <input type="hidden" name="user_id" value={drawerUser.id} />
          <input type="hidden" name="active" value={String(!drawerUser.is_active)} />
          <button class="btn ghost block" class:warn={drawerUser.is_active} type="submit">
            {drawerUser.is_active ? 'Deactivate' : 'Activate'}
          </button>
        </form>
      </div>

      <!-- Service ban toggle -->
      <div class="field">
        <span class="field-label">Service access</span>
        <form method="POST" action={drawerUser.banned ? '?/unban' : '?/ban'} use:enhance={refresh}>
          <input type="hidden" name="user_id" value={drawerUser.id} />
          <button class="btn ghost block" class:warn={!drawerUser.banned} type="submit">
            {drawerUser.banned ? 'Unban from service' : 'Ban from service'}
          </button>
        </form>
      </div>

      <!-- View as (impersonate) -->
      <div class="field">
        <span class="field-label">View as</span>
        <form method="POST" action="?/impersonate" use:enhance={refresh}>
          <input type="hidden" name="user_id" value={drawerUser.id} />
          <button class="btn ghost block" type="submit"><Icon name="users" size={13} /> View as this user</button>
        </form>
        {#if viewAsUrl}
          <div class="viewas-row">
            <input class="viewas-url" type="text" readonly value={viewAsUrl} />
            <button class="btn ghost" type="button" onclick={copyViewAs}>
              <Icon name="link" size={13} /> {copied ? 'Copied' : 'Copy'}
            </button>
          </div>
          <p class="notice-muted">Open this link to load the broadcaster's dashboard. Expires in 5 minutes; every change is audited.</p>
        {/if}
      </div>

      <!-- Maintenance actions -->
      <div class="field">
        <span class="field-label">Maintenance</span>
        <div class="action-stack">
          <form method="POST" action="?/restart" use:enhance={refresh}>
            <input type="hidden" name="user_id" value={drawerUser.id} />
            <button class="btn ghost block" type="submit"><Icon name="pulse" size={13} /> Restart bot</button>
          </form>
          <form method="POST" action="?/reset" use:enhance={refresh}>
            <input type="hidden" name="user_id" value={drawerUser.id} />
            <button class="btn ghost block" type="submit">Reset</button>
          </form>
          <form method="POST" action="?/clearToken" use:enhance={refresh}>
            <input type="hidden" name="user_id" value={drawerUser.id} />
            <button class="btn ghost block" type="submit">Clear token</button>
          </form>
        </div>
      </div>

      <!-- Danger -->
      <div class="field danger-zone">
        <span class="field-label">Danger zone</span>
        <button
          class="btn danger block"
          type="button"
          onclick={() => openDelete({ id: drawerUser.id, username: drawerUser.username })}
        >
          <Icon name="trash" size={13} /> Delete user
        </button>
      </div>
    </div>
  </div>
{/if}

<!-- Delete confirm modal -->
<Modal open={deleteTarget !== null} title={`Delete @${deleteTarget?.username}?`} closeModal={closeDelete}>
  {#if deleteTarget}
    <p class="modal-body">
      This permanently removes the user and cascades to their commands and modules. This cannot be undone.
    </p>
    <form
      method="POST"
      action="?/delete"
      use:enhance={() => async ({ formData, result, update }) => {
        await update({ invalidateAll: false });
        const ok = (result.type === 'success' &&
          (result.data as { action?: { ok?: boolean } } | undefined)?.action?.ok) === true;
        if (ok) {
          const id = String(formData.get('user_id') ?? '');
          removeRecent(id);
          if (selected && String(selected.id) === id) closeDrawer();
        }
        closeDelete();
      }}
      class="modal-actions"
    >
      <input type="hidden" name="user_id" value={deleteTarget.id} />
      <button class="btn ghost" type="button" onclick={closeDelete}>Cancel</button>
      <button class="btn danger" type="submit">
        <Icon name="trash" size={13} /> Delete permanently
      </button>
    </form>
  {/if}
</Modal>

<style>
  /* notice text variants */
  .notice-muted { font-size: .85rem; color: var(--bb-muted); margin: .8rem 0 0; }
  .notice-ok  { font-size: .82rem; color: var(--bb-green-glow); margin: 0 0 .2rem; }
  .notice-err { font-size: .82rem; color: #cf8a78; margin: 0 0 .2rem; }

  /* banned badge */
  .badge.banned {
    margin-left: .4rem;
    background: rgba(176, 90, 70, 0.18); color: #cf8a78;
    border: 1px solid rgba(176, 90, 70, 0.4);
  }

  /* view-as link row */
  .viewas-row { display: flex; gap: .5rem; margin-top: .55rem; }
  .viewas-url {
    flex: 1; min-width: 0;
    font-family: var(--bb-font-mono); font-size: 12px; color: var(--bb-tan-light);
    background: rgba(255,255,255,0.025);
    border: 1px solid var(--glass-border); border-radius: var(--bb-radius-sm, 8px);
    padding: 8px 10px;
  }

  /* lookup bar */
  :global(.canvas:has(.users-screen)) {
    max-width: 1480px;
  }

  .users-screen {
    width: 100%;
    display: flex;
    flex-direction: column;
    gap: var(--row-gap, 20px);
  }

  /* In flex layout margins don't collapse — cancel the global page-head
     bottom margin so the flex gap alone drives the spacing. */
  .users-screen .page-head {
    margin-bottom: 0;
  }

  .lookup-card {
    padding: 18px 20px;
  }
  .lookup-form { display: flex; gap: .6rem; flex-wrap: wrap; align-items: center; }
  .lookup-input { flex: 1 1 420px; min-width: min(100%, 280px); }

  .recent-card {
    padding: 20px 10px;
  }

  /* card-head with filter input */
  .card-head { align-items: center; }
  .recent-head {
    padding: 0 14px;
    gap: .8rem;
  }
  .users-controls {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
    flex: 1;
    min-width: 0;
    margin-left: auto;
  }
  .search-filter {
    margin-left: auto;
    max-width: 320px;
    flex: 1 1 240px;
    min-width: 180px;
  }
  .search-submit,
  .clear-search {
    padding: 10px 14px;
    text-decoration: none;
  }

  .users-table .thead,
  .users-table .trow {
    grid-template-columns: minmax(180px, 1.6fr) minmax(150px, 1.1fr) minmax(110px, .7fr) minmax(110px, .7fr) minmax(90px, .55fr) 44px;
  }

  .users-table .trow {
    min-height: 58px;
  }

  /* clickable table row */
  .trow-clickable { cursor: pointer; user-select: none; transition: background var(--bb-dur-fast, 140ms) var(--bb-ease-out-expo, ease); }
  .trow-clickable:hover { background: rgba(201, 168, 124, 0.06); }
  .trow-clickable:focus-visible { outline: 2px solid var(--bb-tan, #c9a87c); outline-offset: -2px; }
  .trow-clickable.selected { background: rgba(201, 168, 124, 0.12); }

  /* chevron indicator */
  .chev {
    display: inline-block; width: 0; height: 0;
    border-top: 4px solid transparent;
    border-bottom: 4px solid transparent;
    border-left: 5px solid var(--bb-muted, rgba(255,255,255,0.4));
    vertical-align: middle;
  }

  .users-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 14px 18px 0;
    margin-top: 12px;
    border-top: 1px solid var(--glass-border);
  }
  .page-state {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--bb-tan-light);
  }
  .page-state .muted { color: var(--bb-muted); }
  .pager { display: flex; align-items: center; gap: 8px; }
  .pager-link {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 86px;
    padding: 9px 13px;
    border-radius: var(--bb-radius-pill);
    border: 1px solid var(--glass-border);
    background: rgba(255,255,255,0.03);
    color: var(--bb-tan-light);
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    text-decoration: none;
    transition: all var(--bb-dur-base) var(--bb-ease-out-expo);
  }
  .pager-link:hover {
    background: rgba(201,168,124,0.08);
    border-color: var(--bb-border-strong);
    color: var(--bb-tan-pale);
  }
  .pager-link.disabled {
    opacity: 0.42;
    pointer-events: none;
  }

  /* ---- Detail drawer ---- */
  .drawer-backdrop {
    position: fixed; inset: 0; z-index: 190;
    padding: 0; border: 0; cursor: pointer;
    background: rgba(0, 0, 0, 0.5);
    backdrop-filter: blur(2px); -webkit-backdrop-filter: blur(2px);
    animation: fade var(--bb-dur-fast, 160ms) var(--bb-ease-out-expo, ease) both;
  }
  @keyframes fade { from { opacity: 0; } to { opacity: 1; } }

  .drawer {
    position: fixed; top: 0; right: 0; z-index: 191;
    height: 100vh; width: min(620px, 92vw);
    display: flex; flex-direction: column;
    background:
      linear-gradient(var(--glass-fill), var(--glass-fill)),
      var(--bb-bg-1, #111);
    border-left: 1px solid var(--glass-border);
    backdrop-filter: blur(var(--glass-blur)); -webkit-backdrop-filter: blur(var(--glass-blur));
    box-shadow: -16px 0 48px rgba(0, 0, 0, 0.45);
    transform: translateX(100%);
    animation: slide-in var(--bb-dur-med, 320ms) var(--bb-ease-out-expo, cubic-bezier(.16,1,.3,1)) forwards;
  }
  @keyframes slide-in { to { transform: translateX(0); } }

  .drawer-head {
    display: flex; align-items: flex-start; justify-content: space-between;
    gap: 1rem; padding: 22px 22px 16px;
    border-bottom: 1px solid var(--glass-border);
  }
  .drawer-id h2 {
    font-family: var(--bb-font-display); font-weight: 700; font-size: 20px;
    color: var(--bb-white); margin: 0 0 4px; letter-spacing: -0.01em;
  }
  .drawer-sub { font-family: var(--bb-font-mono); font-size: 12px; color: var(--bb-muted); }
  .drawer-close {
    display: inline-flex; align-items: center; justify-content: center;
    width: 32px; height: 32px; flex: none;
    border: 1px solid var(--glass-border); border-radius: var(--bb-radius-sm, 8px);
    background: transparent; color: var(--bb-muted); cursor: pointer;
    transition: all var(--bb-dur-fast, 140ms) var(--bb-ease-out-expo, ease);
  }
  .drawer-close:hover { color: var(--bb-white); border-color: var(--bb-border-strong); background: rgba(255,255,255,0.04); }

  /* min-height:0 lets this flex child actually scroll instead of overflowing. */
  .drawer-body {
    flex: 1; min-height: 0; overflow-y: auto; overscroll-behavior: contain;
    -webkit-overflow-scrolling: touch;
    padding: 20px 22px 32px;
  }

  .meta-block {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: .65rem 1rem;
    padding: 14px 16px; margin-bottom: 18px;
    background: rgba(255,255,255,0.025);
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-md, 12px);
  }
  .meta-line { display: flex; align-items: center; justify-content: space-between; gap: 1rem; }
  .meta-k { font-size: 12px; color: var(--bb-muted); text-transform: uppercase; letter-spacing: .05em; }
  .meta-v { font-family: var(--bb-font-mono); font-size: 13px; color: var(--bb-tan-light); }

  .field { margin-bottom: 18px; }
  .field-label {
    display: block; font-size: 11px; text-transform: uppercase; letter-spacing: .06em;
    color: var(--bb-muted); margin-bottom: .55rem;
  }
  .field form { display: block; }
  .action-stack { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: .5rem; }

  /* tier segment */
  .segment {
    display: grid; grid-template-columns: repeat(3, 1fr); gap: .4rem;
  }
  .segment form { display: block; }
  .seg-btn {
    width: 100%; padding: 10px 8px; cursor: pointer;
    font-family: var(--bb-font-body); font-size: 13px; font-weight: 600;
    color: var(--bb-muted);
    background: transparent;
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-sm, 8px);
    transition: all var(--bb-dur-fast, 140ms) var(--bb-ease-out-expo, ease);
  }
  .seg-btn:hover:not(:disabled) { color: var(--bb-white); border-color: var(--bb-border-strong); background: rgba(255,255,255,0.04); }
  .seg-btn:active:not(:disabled) { transform: translateY(1px); }
  .seg-btn.on {
    color: var(--bb-bg-1, #111); font-weight: 700;
    background: var(--bb-tan, #c9a87c);
    border-color: var(--bb-tan, #c9a87c);
    cursor: default;
  }
  .seg-btn:disabled:not(.on) { opacity: .5; cursor: not-allowed; }

  /* full-width buttons */
  .btn.block { width: 100%; display: inline-flex; align-items: center; justify-content: center; gap: .4rem; }
  .btn.ghost.warn { color: #cf8a78; border-color: rgba(176, 90, 70, 0.35); }
  .btn.ghost.warn:hover { color: #e09e8a; border-color: rgba(176, 90, 70, 0.55); background: rgba(176, 90, 70, 0.12); }

  .danger-zone { margin-top: 6px; padding-top: 16px; border-top: 1px solid var(--glass-border); }

  /* danger button variant */
  .btn.danger {
    background: rgba(176, 90, 70, 0.12); color: #cf8a78;
    border-color: rgba(176, 90, 70, 0.35);
  }
  .btn.danger:hover {
    background: rgba(176, 90, 70, 0.22); color: #e09e8a;
    border-color: rgba(176, 90, 70, 0.55);
  }
  .btn.danger:disabled { opacity: .45; cursor: not-allowed; }



  /* mobile responsive */
  @media (max-width: 760px) {
    :global(.canvas:has(.users-screen)) {
      max-width: 1180px;
    }

    .lookup-card {
      margin-bottom: 14px;
    }

    .recent-card {
      padding: 18px 6px;
    }

    .recent-head { align-items: stretch; flex-direction: column; }
    .users-controls { width: 100%; margin-left: 0; justify-content: flex-start; flex-wrap: wrap; }
    .search-filter { max-width: none; min-width: 100%; margin-left: 0; }
    .search-submit, .clear-search { flex: 1; justify-content: center; }
    :global(.stat-grid) { grid-template-columns: 1fr 1fr; }

    /* The shared .trow mobile collapse is tuned for the commands table (hides
       resp/cd/perm-cell, 1fr auto). For users those classes carry id/status/tier,
       so the row squashes to 2 cols with no room. Give the users table its own
       readable mobile row: @username · tier · status, with id/active/chevron
       dropped. */
    .users-table .thead { display: none; }
    .users-table .trow {
      grid-template-columns: 1fr auto auto;
      gap: 12px;
      align-items: center;
    }
    .users-table .trow .resp,
    .users-table .trow .uses,
    .users-table .trow .row-act { display: none; }
    .users-table .trow .perm-cell,
    .users-table .trow .cd { display: revert; }
    .users-foot { align-items: stretch; flex-direction: column; padding-inline: 14px; }
    .pager { display: grid; grid-template-columns: 1fr 1fr; width: 100%; }
    .pager-link { min-width: 0; }
    .drawer {
      width: 100vw; height: 92vh; top: auto; bottom: 0; right: 0;
      border-left: none; border-top: 1px solid var(--glass-border);
      border-radius: var(--bb-radius-lg, 16px) var(--bb-radius-lg, 16px) 0 0;
      transform: translateY(100%);
      animation: sheet-in var(--bb-dur-med, 320ms) var(--bb-ease-out-expo, cubic-bezier(.16,1,.3,1)) forwards;
    }
    .meta-block {
      grid-template-columns: 1fr;
    }
    .action-stack {
      grid-template-columns: 1fr;
    }
    @keyframes sheet-in { to { transform: translateY(0); } }
  }

  @media (max-width: 380px) {
    .btn { font-size: 10px; padding: 10px 14px; }
  }
</style>
