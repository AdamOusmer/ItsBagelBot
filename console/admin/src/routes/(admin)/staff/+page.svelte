<script lang="ts">
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import { Icon, Button } from '@bagel/shared';

  type AdminRole = 'moderator' | 'admin' | 'owner';
  type AdminAcct = {
    id: number;
    login: string;
    display_name: string;
    role: AdminRole;
    active: boolean;
    added_by: number;
    created_at: string;
  };
  type AuditEntry = {
    id: number;
    actor_id: number;
    actor_login: string;
    action: string;
    target?: string;
    detail?: string;
    ok: boolean;
    error?: string;
    created_at: string;
  };

  let { data, form } = $props();

  const action = $derived(form?.action as { ok: boolean; notice: string } | undefined);

  function refresh() {
    return async ({ update }: { update: () => Promise<void> }) => {
      await update();
      await invalidateAll();
    };
  }

  // Role ladder — mirror of the server. Server re-enforces; this is UX only.
  function canManage(actor: AdminRole, target: AdminRole): boolean {
    if (actor !== 'admin' && actor !== 'owner') return false;
    if (target === 'owner') return actor === 'owner';
    const rank = { moderator: 1, admin: 2, owner: 3 };
    return rank[actor] >= rank[target];
  }

  const roleOptions = $derived<AdminRole[]>(
    data.me.role === 'owner' ? ['moderator', 'admin', 'owner'] : ['moderator', 'admin']
  );

  function isSelf(row: AdminAcct): boolean {
    return String(row.id) === data.me.id;
  }

  // --- Add / promote form state ---------------------------------------
  let addRole = $state<AdminRole>('moderator');

  // --- Roster filter --------------------------------------------------
  let filter = $state('');
  const rows = $derived(
    (data.staff as AdminAcct[]).filter((s) => {
      const q = filter.trim().toLowerCase();
      return !q || s.login.toLowerCase().includes(q) || String(s.id).includes(q);
    })
  );

  // --- Selected member (drawer) ---------------------------------------
  let selected = $state<AdminAcct | null>(null);
  // Re-resolve from fresh data so role/status reflect mutations immediately.
  const drawer = $derived.by<AdminAcct | null>(() => {
    if (!selected) return null;
    return (data.staff as AdminAcct[]).find((s) => String(s.id) === String(selected!.id)) ?? selected;
  });
  const manageable = $derived(drawer ? canManage(data.me.role, drawer.role) : false);

  // That member's own action history, newest first (server sends newest-first).
  const history = $derived.by<AuditEntry[]>(() => {
    if (!drawer) return [];
    return ((data.audit as AuditEntry[]) ?? []).filter((e) => String(e.actor_id) === String(drawer.id));
  });

  function openMember(row: AdminAcct) {
    selected = row;
  }
  function closeDrawer() {
    selected = null;
  }
  function handleRowKey(e: KeyboardEvent, row: AdminAcct) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      openMember(row);
    }
  }

  // Role change (inside drawer): submit on select change.
  let roleFormEl = $state<HTMLFormElement | null>(null);
  function submitRoleChange() {
    roleFormEl?.requestSubmit();
  }

  // --- Confirm-remove modal -------------------------------------------
  let removeTarget = $state<AdminAcct | null>(null);
  let removeFormEl = $state<HTMLFormElement | null>(null);
  function openRemove(row: AdminAcct) {
    removeTarget = row;
  }
  function closeRemove() {
    removeTarget = null;
  }
  function confirmRemove() {
    if (removeFormEl) removeFormEl.requestSubmit();
    closeRemove();
  }

  function handleKey(e: KeyboardEvent) {
    if (e.key !== 'Escape') return;
    if (removeTarget) closeRemove();
    else if (selected) closeDrawer();
  }

  function roleBadge(role: AdminRole): string {
    if (role === 'owner') return 'owner';
    if (role === 'admin') return 'sub';
    return 'everyone';
  }

  function relDate(iso: string): string {
    const then = new Date(iso).getTime();
    if (Number.isNaN(then)) return '—';
    const secs = Math.max(0, Math.round((Date.now() - then) / 1000));
    if (secs < 60) return 'just now';
    const mins = Math.round(secs / 60);
    if (mins < 60) return `${mins}m ago`;
    const hrs = Math.round(mins / 60);
    if (hrs < 24) return `${hrs}h ago`;
    const days = Math.round(hrs / 24);
    if (days < 30) return `${days}d ago`;
    const months = Math.round(days / 30);
    if (months < 12) return `${months}mo ago`;
    return `${Math.round(months / 12)}y ago`;
  }
</script>

<svelte:window onkeydown={handleKey} />

<section class="screen active">
  <div class="page-head">
    <span class="eyebrow">Access control</span>
    <h1>Staff <em>management</em></h1>
    <p>
      Operators with console access. Roles: moderator, admin, owner.{#if data.degraded}
        <em> Live staff data unavailable; showing sample.</em>{/if}
    </p>
  </div>

  <!-- Add / promote staff -->
  <div class="card add-card">
    <div class="card-head"><h3>Add / promote staff</h3></div>
    <form method="POST" action="?/upsert" use:enhance={refresh} class="add-form">
      <label class="search add-input">
        <Icon name="symbol" size={14} />
        <input
          name="user_id"
          type="text"
          inputmode="numeric"
          pattern="[0-9]+"
          placeholder="Twitch user id"
          autocomplete="off"
          required
        />
      </label>
      <label class="search add-input">
        <Icon name="users" size={14} />
        <input name="login" type="text" placeholder="Twitch username" autocomplete="off" required />
      </label>
      <label class="search add-input">
        <Icon name="edit" size={14} />
        <input name="display_name" type="text" placeholder="Display name (optional)" autocomplete="off" />
      </label>
      <select class="role-select" name="role" bind:value={addRole} aria-label="Role">
        {#each roleOptions as r}
          <option value={r}>{r}</option>
        {/each}
      </select>
      <Button variant="primary" icon="check" type="submit">Save</Button>
    </form>
    <p class="add-hint">Twitch user id + username of the account to grant access. The username is the
      Twitch login (e.g. <code>itsmavey</code>); it self-updates on their first sign-in.</p>
  </div>

  <!-- Roster -->
  <div class="card" style="padding:18px 6px">
    {#if action}
      <p class="notice-{action.ok ? 'ok' : 'err'}" style="padding:0 14px">{action.notice}</p>
    {/if}

    <div class="card-head" style="padding:0 12px;gap:.6rem">
      <h3>Roster</h3>
      <label class="search search-filter">
        <Icon name="search" size={14} />
        <input type="text" placeholder="Filter by login or id" autocomplete="off" bind:value={filter} />
      </label>
    </div>

    <div class="table staff-table">
      <div class="thead">
        <span>Member</span><span>Role</span><span class="perm-cell">Status</span><span class="perm-cell">Added</span><span></span>
      </div>
      <div class="trows">
        {#if rows.length === 0}
          <div class="trow"><span class="resp" style="grid-column:1/-1;opacity:.6">No matching staff.</span></div>
        {/if}
        {#each rows as row (row.id)}
          <div
            class="trow trow-clickable"
            class:selected={selected && String(selected.id) === String(row.id)}
            role="button"
            tabindex="0"
            onclick={() => openMember(row)}
            onkeydown={(e) => handleRowKey(e, row)}
          >
            <span class="member">
              <span class="cmd">@{row.login}</span>
              {#if isSelf(row)}<span class="you-tag">you</span>{/if}
              <span class="member-sub">{row.display_name}</span>
            </span>
            <span class="role-cell"><span class="badge {roleBadge(row.role)}">{row.role}</span></span>
            <span class="cd perm-cell">{row.active ? 'active' : 'inactive'}</span>
            <span class="cd perm-cell">{relDate(row.created_at)}</span>
            <span class="row-act"><span class="chev" aria-hidden="true"></span></span>
          </div>
        {/each}
      </div>
    </div>
  </div>
</section>

<!-- Member drawer -->
{#if drawer}
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div class="drawer-backdrop" onclick={closeDrawer}></div>
  <div class="drawer open" role="dialog" aria-modal="true" aria-labelledby="staff-drawer-title">
    <header class="drawer-head">
      <div class="drawer-id">
        <h2 id="staff-drawer-title">@{drawer.login}</h2>
        <span class="drawer-sub">id {drawer.id} · {drawer.role}</span>
      </div>
      <button class="drawer-close" type="button" onclick={closeDrawer} aria-label="Close">
        <Icon name="x" size={16} />
      </button>
    </header>

    <div class="drawer-body">
      <div class="meta-block">
        <div class="meta-line"><span class="meta-k">Display</span><span class="meta-v">{drawer.display_name}</span></div>
        <div class="meta-line"><span class="meta-k">Role</span><span class="badge {roleBadge(drawer.role)}">{drawer.role}</span></div>
        <div class="meta-line"><span class="meta-k">Status</span><span class="meta-v">{drawer.active ? 'active' : 'inactive'}</span></div>
        <div class="meta-line"><span class="meta-k">Added</span><span class="meta-v">{relDate(drawer.created_at)}</span></div>
      </div>

      {#if manageable && !isSelf(drawer)}
        {@const target = drawer}
        <div class="field">
          <span class="field-label">Role</span>
          <form method="POST" action="?/upsert" use:enhance={refresh} bind:this={roleFormEl}>
            <input type="hidden" name="user_id" value={target.id} />
            <input type="hidden" name="login" value={target.login} />
            <input type="hidden" name="display_name" value={target.display_name} />
            <select class="role-select block" name="role" value={target.role} onchange={submitRoleChange} aria-label="Change role">
              {#each roleOptions as r}
                <option value={r}>{r}</option>
              {/each}
              {#if !roleOptions.includes(target.role)}
                <option value={target.role}>{target.role}</option>
              {/if}
            </select>
          </form>
        </div>

        <div class="field danger-zone">
          <span class="field-label">Danger zone</span>
          <button class="btn danger block" type="button" onclick={() => openRemove(target)}>
            <Icon name="trash" size={13} /> Remove from staff
          </button>
        </div>
      {:else}
        <p class="ro-note">
          {isSelf(drawer) ? 'This is your own account.' : 'Read-only: you cannot manage this member.'}
        </p>
      {/if}

      <!-- Action history made by this member -->
      <div class="field">
        <span class="field-label">History</span>
        {#if history.length === 0}
          <p class="hist-empty">No recorded actions.</p>
        {:else}
          <div class="hist">
            {#each history as e (e.id)}
              <div class="hist-row">
                <span class="hist-act">{e.action}</span>
                <span class="hist-meta">
                  {#if e.target}<span class="hist-target">{e.target}</span>{/if}
                  {#if e.detail}<span class="hist-detail">{e.detail}</span>{/if}
                </span>
                <span class="hist-when" class:err={!e.ok}>{e.ok ? relDate(e.created_at) : 'failed'}</span>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    </div>
  </div>
{/if}

<!-- Remove confirm modal -->
{#if removeTarget}
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div class="modal-backdrop" onclick={closeRemove} role="dialog" aria-modal="true" aria-labelledby="rm-title" tabindex="-1">
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <div class="modal-card" role="presentation" onclick={(e) => e.stopPropagation()}>
      <h3 id="rm-title">Remove @{removeTarget.login} from staff?</h3>
      <p class="modal-body">
        This deactivates their console access. They will no longer be able to sign in or manage the bot.
      </p>
      <form method="POST" action="?/remove" use:enhance={refresh} bind:this={removeFormEl} style="display:none">
        <input type="hidden" name="user_id" value={removeTarget.id} />
        <input type="hidden" name="target_role" value={removeTarget.role} />
      </form>
      <div class="modal-actions">
        <button class="btn ghost" type="button" onclick={closeRemove}>Cancel</button>
        <button class="btn danger" type="button" onclick={confirmRemove}>
          <Icon name="trash" size={13} /> Remove
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .notice-ok { font-size: 0.82rem; color: var(--bb-green-glow); margin: 0 0 0.4rem; }
  .notice-err { font-size: 0.82rem; color: #cf8a78; margin: 0 0 0.4rem; }

  /* add / promote card */
  .add-card { padding: 18px 20px; margin-bottom: var(--row-gap, 20px); }
  .add-card .card-head { margin-bottom: 14px; }
  .add-form { display: flex; gap: 0.6rem; flex-wrap: wrap; align-items: center; }
  .add-input { flex: 1; min-width: 140px; }
  .add-hint { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); margin: 12px 0 0; line-height: 1.5; }
  .add-hint code { font-family: var(--bb-font-mono); color: var(--bb-tan-light); }

  /* role select */
  .role-select {
    font-family: var(--bb-font-body); font-size: 13px; color: var(--bb-white);
    background: rgba(0, 0, 0, 0.25); border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-pill, 999px); padding: 9px 14px; cursor: pointer;
    appearance: none; -webkit-appearance: none; outline: 0;
    transition: border-color var(--bb-dur-fast, 140ms) var(--bb-ease-out-expo, ease);
  }
  .role-select:hover, .role-select:focus-visible { border-color: var(--bb-border-strong); }
  .role-select.block { width: 100%; border-radius: var(--bb-radius-sm, 8px); }
  .role-select option { color: #111; }

  /* filter input */
  .card-head { align-items: center; }
  .search-filter { margin-left: auto; max-width: 220px; flex: 1; min-width: 0; }

  /* staff table — own 5-col grid (the shared .trow is 6-col) */
  .staff-table .thead, .staff-table .trow {
    grid-template-columns: 2fr 1fr 0.8fr 0.8fr 40px;
  }

  /* member cell */
  .member { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .member .cmd { font-family: var(--bb-font-mono); font-size: 13.5px; color: var(--bb-tan-light); font-weight: 500; }
  .member-sub { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .you-tag {
    align-self: flex-start;
    font-family: var(--bb-font-mono); font-size: 9px; letter-spacing: 0.1em; text-transform: uppercase;
    color: var(--bb-green-glow); background: rgba(82, 183, 136, 0.1);
    border: 1px solid rgba(82, 183, 136, 0.28); border-radius: var(--bb-radius-pill, 999px); padding: 2px 7px;
  }
  .role-cell { display: flex; align-items: center; }

  /* owner badge */
  .badge.owner { background: rgba(82, 183, 136, 0.16); color: var(--bb-green-glow); border-color: rgba(82, 183, 136, 0.4); }

  /* clickable rows */
  .trow-clickable { cursor: pointer; user-select: none; transition: background var(--bb-dur-fast, 140ms) var(--bb-ease-out-expo, ease); }
  .trow-clickable:hover { background: rgba(201, 168, 124, 0.06); }
  .trow-clickable:focus-visible { outline: 2px solid var(--bb-tan, #c9a87c); outline-offset: -2px; }
  .trow-clickable.selected { background: rgba(201, 168, 124, 0.12); }
  .row-act { display: flex; align-items: center; justify-content: flex-end; }
  .chev {
    display: inline-block; width: 0; height: 0;
    border-top: 4px solid transparent; border-bottom: 4px solid transparent;
    border-left: 5px solid var(--bb-muted, rgba(255,255,255,0.4)); vertical-align: middle;
  }

  /* ---- drawer (mirrors users page) ---- */
  .drawer-backdrop {
    position: fixed; inset: 0; z-index: 190; background: rgba(0, 0, 0, 0.5);
    backdrop-filter: blur(2px); -webkit-backdrop-filter: blur(2px);
    animation: fade var(--bb-dur-fast, 160ms) var(--bb-ease-out-expo, ease) both;
  }
  @keyframes fade { from { opacity: 0; } to { opacity: 1; } }
  .drawer {
    position: fixed; top: 0; right: 0; z-index: 191; height: 100vh; width: min(420px, 92vw);
    display: flex; flex-direction: column;
    background: linear-gradient(var(--glass-fill), var(--glass-fill)), var(--bb-bg-1, #111);
    border-left: 1px solid var(--glass-border);
    backdrop-filter: blur(var(--glass-blur)); -webkit-backdrop-filter: blur(var(--glass-blur));
    box-shadow: -16px 0 48px rgba(0, 0, 0, 0.45);
    transform: translateX(100%);
    animation: slide-in var(--bb-dur-med, 320ms) var(--bb-ease-out-expo, cubic-bezier(.16,1,.3,1)) forwards;
  }
  @keyframes slide-in { to { transform: translateX(0); } }
  .drawer-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 1rem; padding: 22px 22px 16px; border-bottom: 1px solid var(--glass-border); }
  .drawer-id h2 { font-family: var(--bb-font-display); font-weight: 700; font-size: 20px; color: var(--bb-white); margin: 0 0 4px; letter-spacing: -0.01em; }
  .drawer-sub { font-family: var(--bb-font-mono); font-size: 12px; color: var(--bb-muted); }
  .drawer-close {
    display: inline-flex; align-items: center; justify-content: center; width: 32px; height: 32px; flex: none;
    border: 1px solid var(--glass-border); border-radius: var(--bb-radius-sm, 8px);
    background: transparent; color: var(--bb-muted); cursor: pointer;
    transition: all var(--bb-dur-fast, 140ms) var(--bb-ease-out-expo, ease);
  }
  .drawer-close :global(svg) { stroke: currentColor; }
  .drawer-close:hover { color: var(--bb-white); border-color: var(--bb-border-strong); background: rgba(255,255,255,0.04); }
  .drawer-body { flex: 1; overflow-y: auto; padding: 20px 22px 32px; }

  .meta-block {
    display: grid; gap: .5rem; padding: 14px 16px; margin-bottom: 18px;
    background: rgba(255,255,255,0.025); border: 1px solid var(--glass-border); border-radius: var(--bb-radius-md, 12px);
  }
  .meta-line { display: flex; align-items: center; justify-content: space-between; gap: 1rem; }
  .meta-k { font-size: 12px; color: var(--bb-muted); text-transform: uppercase; letter-spacing: .05em; }
  .meta-v { font-family: var(--bb-font-mono); font-size: 13px; color: var(--bb-tan-light); }

  .field { margin-bottom: 18px; }
  .field-label { display: block; font-size: 11px; text-transform: uppercase; letter-spacing: .06em; color: var(--bb-muted); margin-bottom: .55rem; }
  .ro-note { font-family: var(--bb-font-body); font-size: 13px; color: var(--bb-muted); margin: 0 0 18px; }
  .danger-zone { margin-top: 6px; padding-top: 16px; border-top: 1px solid var(--glass-border); }

  .btn.block { width: 100%; display: inline-flex; align-items: center; justify-content: center; gap: .4rem; }
  .btn.danger { background: rgba(176, 90, 70, 0.12); color: #cf8a78; border-color: rgba(176, 90, 70, 0.35); }
  .btn.danger:hover:not(:disabled) { background: rgba(176, 90, 70, 0.22); color: #e09e8a; border-color: rgba(176, 90, 70, 0.55); }

  /* history list */
  .hist { display: flex; flex-direction: column; gap: 2px; }
  .hist-row { display: grid; grid-template-columns: auto 1fr auto; gap: .6rem; align-items: baseline; padding: 8px 2px; border-bottom: 1px solid var(--glass-border); }
  .hist-row:last-child { border-bottom: 0; }
  .hist-act { font-family: var(--bb-font-mono); font-size: 12.5px; color: var(--bb-tan-light); }
  .hist-meta { display: flex; gap: .5rem; min-width: 0; flex-wrap: wrap; }
  .hist-target { font-family: var(--bb-font-mono); font-size: 11.5px; color: var(--bb-muted); }
  .hist-detail { font-family: var(--bb-font-body); font-size: 11.5px; color: var(--bb-muted); opacity: .8; }
  .hist-when { font-family: var(--bb-font-mono); font-size: 11px; color: var(--bb-muted); white-space: nowrap; }
  .hist-when.err { color: #cf8a78; }
  .hist-empty { font-family: var(--bb-font-body); font-size: 13px; color: var(--bb-muted); margin: 0; }

  /* confirm modal */
  .modal-backdrop {
    position: fixed; inset: 0; z-index: 200; background: rgba(0, 0, 0, 0.55);
    backdrop-filter: blur(4px); -webkit-backdrop-filter: blur(4px);
    display: flex; align-items: center; justify-content: center; padding: 16px;
    animation: fade var(--bb-dur-fast, 160ms) var(--bb-ease-out-expo, ease) both;
  }
  .modal-card {
    background: var(--bb-bg-1, #111); border: 1px solid var(--glass-border); border-radius: var(--bb-radius-lg);
    backdrop-filter: blur(var(--glass-blur)); -webkit-backdrop-filter: blur(var(--glass-blur));
    padding: 28px 28px 24px; max-width: 420px; width: 100%;
  }
  .modal-card h3 { font-family: var(--bb-font-display); font-weight: 700; font-size: 19px; color: var(--bb-white); margin: 0 0 12px; letter-spacing: -0.01em; }
  .modal-body { font-family: var(--bb-font-body); font-size: 14px; color: var(--bb-muted); line-height: 1.55; margin: 0 0 22px; }
  .modal-actions { display: flex; gap: 0.6rem; justify-content: flex-end; flex-wrap: wrap; }

  /* mobile */
  @media (max-width: 760px) {
    .add-form { flex-direction: column; align-items: stretch; }
    .add-input { min-width: 0; }
    .role-select { width: 100%; }
    .search-filter { max-width: 160px; }
    .staff-table .thead { display: none; }
    .staff-table .trow { grid-template-columns: 1fr auto; gap: 8px; }
    .staff-table .trow .perm-cell { display: none; }
    .drawer {
      width: 100vw; height: 92vh; top: auto; bottom: 0; right: 0;
      border-left: none; border-top: 1px solid var(--glass-border);
      border-radius: var(--bb-radius-lg, 16px) var(--bb-radius-lg, 16px) 0 0;
      transform: translateY(100%);
      animation: sheet-in var(--bb-dur-med, 320ms) var(--bb-ease-out-expo, cubic-bezier(.16,1,.3,1)) forwards;
    }
    @keyframes sheet-in { to { transform: translateY(0); } }
  }
  @media (max-width: 380px) { .modal-card { padding: 20px 16px 18px; } }
</style>
