<script lang="ts">
  import { enhance } from '$app/forms';
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

  let { data, form } = $props();

  const action = $derived(form?.action as { ok: boolean; notice: string } | undefined);

  // staffUpsert/staffRemove reply with the authoritative roster, so the action
  // result carries the fresh list. Prefer it over the loaded data and skip the
  // post-mutation invalidateAll refetch — that refetch both stalled the
  // response and, under concurrent submits, raced the load against the write.
  const roster = $derived((form?.staff as AdminAcct[] | undefined) ?? (data.staff as AdminAcct[]));

  // Apply the result without invalidateAll: update() refreshes `form` (notice +
  // roster) but does not re-run load.
  function refresh() {
    return async ({ update }: { update: (opts?: { invalidateAll?: boolean }) => Promise<void> }) => {
      await update({ invalidateAll: false });
    };
  }

  // Role ladder — mirror of the server. Server re-enforces; this is UX only.
  function canManage(actor: AdminRole, target: AdminRole): boolean {
    if (actor !== 'admin' && actor !== 'owner') return false;
    if (target === 'owner') return actor === 'owner';
    const rank = { moderator: 1, admin: 2, owner: 3 };
    return rank[actor] >= rank[target];
  }

  // Options the signed-in admin may grant. Owner is gated to owners.
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
    roster.filter((s) => {
      const q = filter.trim().toLowerCase();
      return !q || s.login.toLowerCase().includes(q) || String(s.id).includes(q);
    })
  );

  // --- Per-row role-change forms (submit on select change) ------------
  let roleFormEls = $state<Record<string, HTMLFormElement | undefined>>({});
  function submitRoleChange(id: number) {
    roleFormEls[String(id)]?.requestSubmit();
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
    if (e.key === 'Escape' && removeTarget) closeRemove();
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
        <input name="login" type="text" placeholder="login" autocomplete="off" required />
      </label>
      <label class="search add-input">
        <Icon name="edit" size={14} />
        <input name="display_name" type="text" placeholder="display name (optional)" autocomplete="off" />
      </label>
      <select class="role-select" name="role" bind:value={addRole} aria-label="Role">
        {#each roleOptions as r}
          <option value={r}>{r}</option>
        {/each}
      </select>
      <Button variant="primary" icon="check" type="submit">Save</Button>
    </form>
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

    <div class="table">
      <div class="thead">
        <span>Member</span><span>Role</span><span class="perm-cell">Status</span><span class="perm-cell">Added</span><span></span>
      </div>
      <div class="trows">
        {#if rows.length === 0}
          <div class="trow"><span class="resp" style="grid-column:1/-1;opacity:.6">No matching staff.</span></div>
        {/if}
        {#each rows as row (row.id)}
          {@const manageable = canManage(data.me.role, row.role)}
          <div class="trow">
            <span class="member">
              <span class="cmd">@{row.login}</span>
              {#if isSelf(row)}<span class="you-tag">you</span>{/if}
              <span class="member-sub">{row.display_name}</span>
            </span>

            <span class="role-cell">
              {#if manageable}
                <form
                  method="POST"
                  action="?/upsert"
                  use:enhance={refresh}
                  bind:this={roleFormEls[String(row.id)]}
                  class="role-form"
                >
                  <input type="hidden" name="user_id" value={row.id} />
                  <input type="hidden" name="login" value={row.login} />
                  <input type="hidden" name="display_name" value={row.display_name} />
                  <select
                    class="role-select row"
                    name="role"
                    value={row.role}
                    onchange={() => submitRoleChange(row.id)}
                    aria-label="Change role for @{row.login}"
                  >
                    {#each roleOptions as r}
                      <option value={r}>{r}</option>
                    {/each}
                    {#if !roleOptions.includes(row.role)}
                      <option value={row.role}>{row.role}</option>
                    {/if}
                  </select>
                </form>
              {:else}
                <span class="badge {roleBadge(row.role)}">{row.role}</span>
                <span class="ro-hint">read-only</span>
              {/if}
            </span>

            <span class="cd perm-cell">{row.active ? 'active' : 'inactive'}</span>
            <span class="cd perm-cell">{relDate(row.created_at)}</span>

            <span class="row-act">
              {#if manageable}
                <button
                  class="btn danger sm"
                  type="button"
                  disabled={isSelf(row)}
                  onclick={() => openRemove(row)}
                >
                  <Icon name="trash" size={13} /> Remove
                </button>
              {/if}
            </span>
          </div>
        {/each}
      </div>
    </div>
  </div>
</section>

<!-- Remove confirm modal -->
{#if removeTarget}
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div
    class="modal-backdrop"
    onclick={closeRemove}
    role="dialog"
    aria-modal="true"
    aria-labelledby="rm-title"
    tabindex="-1"
  >
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <div class="modal-card" role="presentation" onclick={(e) => e.stopPropagation()}>
      <h3 id="rm-title">Remove @{removeTarget.login} from staff?</h3>
      <p class="modal-body">
        This deactivates their console access. They will no longer be able to sign in or manage the bot.
      </p>
      <form
        method="POST"
        action="?/remove"
        use:enhance={refresh}
        bind:this={removeFormEl}
        style="display:none"
      >
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
  /* notice text variants */
  .notice-ok { font-size: 0.82rem; color: var(--bb-green-glow); margin: 0 0 0.4rem; }
  .notice-err { font-size: 0.82rem; color: #cf8a78; margin: 0 0 0.4rem; }

  /* add / promote card */
  .add-card { padding: 18px 20px; margin-bottom: var(--row-gap, 20px); }
  .add-card .card-head { margin-bottom: 14px; }
  .add-form { display: flex; gap: 0.6rem; flex-wrap: wrap; align-items: center; }
  .add-input { flex: 1; min-width: 140px; }

  /* role select */
  .role-select {
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-white);
    background: rgba(0, 0, 0, 0.25);
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-pill, 999px);
    padding: 9px 14px;
    cursor: pointer;
    appearance: none;
    -webkit-appearance: none;
    outline: 0;
    transition: border-color var(--bb-dur-fast, 140ms) var(--bb-ease-out-expo, ease);
  }
  .role-select:hover,
  .role-select:focus-visible { border-color: var(--bb-border-strong); }
  .role-select.row { padding: 7px 12px; font-size: 12.5px; }
  .role-select option { color: #111; }

  /* filter input */
  .card-head { align-items: center; }
  .search-filter { margin-left: auto; max-width: 220px; flex: 1; min-width: 0; }

  /* member cell */
  .member { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .member .cmd { font-family: var(--bb-font-mono); font-size: 13.5px; color: var(--bb-tan-light); font-weight: 500; }
  .member-sub { font-family: var(--bb-font-body); font-size: 12px; color: var(--bb-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .you-tag {
    align-self: flex-start;
    font-family: var(--bb-font-mono); font-size: 9px; letter-spacing: 0.1em; text-transform: uppercase;
    color: var(--bb-green-glow);
    background: rgba(82, 183, 136, 0.1);
    border: 1px solid rgba(82, 183, 136, 0.28);
    border-radius: var(--bb-radius-pill, 999px);
    padding: 2px 7px;
  }

  /* role cell */
  .role-cell { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .role-form { display: inline-flex; }
  .ro-hint { font-family: var(--bb-font-mono); font-size: 10px; letter-spacing: 0.06em; color: var(--bb-muted); opacity: 0.7; }

  /* owner badge — strong green tint */
  .badge.owner {
    background: rgba(82, 183, 136, 0.16);
    color: var(--bb-green-glow);
    border-color: rgba(82, 183, 136, 0.4);
  }

  /* actions */
  .row-act { display: flex; align-items: center; justify-content: flex-end; gap: 6px; }
  .btn.sm { padding: 8px 14px; font-size: 10px; }

  /* danger button variant */
  .btn.danger {
    background: rgba(176, 90, 70, 0.12); color: #cf8a78;
    border-color: rgba(176, 90, 70, 0.35);
  }
  .btn.danger:hover:not(:disabled) {
    background: rgba(176, 90, 70, 0.22); color: #e09e8a;
    border-color: rgba(176, 90, 70, 0.55);
  }
  .btn.danger:disabled { opacity: 0.4; cursor: not-allowed; }

  /* confirm modal */
  .modal-backdrop {
    position: fixed; inset: 0; z-index: 200;
    background: rgba(0, 0, 0, 0.55);
    backdrop-filter: blur(4px); -webkit-backdrop-filter: blur(4px);
    display: flex; align-items: center; justify-content: center; padding: 16px;
    animation: fade var(--bb-dur-fast, 160ms) var(--bb-ease-out-expo, ease) both;
  }
  @keyframes fade { from { opacity: 0; } to { opacity: 1; } }
  .modal-card {
    background: var(--bb-bg-1, #111);
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-lg);
    backdrop-filter: blur(var(--glass-blur)); -webkit-backdrop-filter: blur(var(--glass-blur));
    padding: 28px 28px 24px; max-width: 420px; width: 100%;
  }
  .modal-card h3 {
    font-family: var(--bb-font-display); font-weight: 700; font-size: 19px;
    color: var(--bb-white); margin: 0 0 12px; letter-spacing: -0.01em;
  }
  .modal-body {
    font-family: var(--bb-font-body); font-size: 14px; color: var(--bb-muted);
    line-height: 1.55; margin: 0 0 22px;
  }
  .modal-actions { display: flex; gap: 0.6rem; justify-content: flex-end; flex-wrap: wrap; }

  /* mobile */
  @media (max-width: 760px) {
    .add-form { flex-direction: column; align-items: stretch; }
    .add-input { min-width: 0; }
    .role-select { width: 100%; }
    .search-filter { max-width: 160px; }
    .thead { display: none; }
    .trow { grid-template-columns: 1fr auto; gap: 8px; }
    .trow .perm-cell { display: none; }
  }

  @media (max-width: 380px) {
    .modal-card { padding: 20px 16px 18px; }
  }
</style>
