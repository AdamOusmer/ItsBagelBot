<script lang="ts">
  import { enhance } from '$app/forms';
  import { Icon, StatTile, Button } from '@bagel/shared';
  import type { AdminUserWire } from '$lib/server/rpc';
  let { data, form } = $props();

  const lookup = $derived(form?.lookup as Record<string, unknown> | undefined);
  const found = $derived(lookup?.user as AdminUserWire | undefined);
  const lookupError = $derived(lookup?.error as string | undefined);
  const tokenPresent = $derived(Boolean(lookup?.tokenPresent));
  const action = $derived(form?.action as { ok: boolean; notice: string } | undefined);

  function tier(status: string): 'premium' | 'standard' {
    return status === 'paid' || status === 'vip' ? 'premium' : 'standard';
  }

  // Confirm-delete modal state
  let deleteTarget = $state<{ id: number | string; username: string } | null>(null);
  let deleteFormEl = $state<HTMLFormElement | null>(null);

  function openDelete(u: { id: number | string; username: string }) {
    deleteTarget = u;
  }

  function closeDelete() {
    deleteTarget = null;
  }

  function confirmDelete() {
    if (deleteFormEl) deleteFormEl.requestSubmit();
    closeDelete();
  }

  function handleModalKey(e: KeyboardEvent) {
    if (e.key === 'Escape') closeDelete();
  }
</script>

<svelte:window onkeydown={handleModalKey} />

<section class="screen active">
  <div class="page-head">
    <span class="eyebrow">Broadcaster accounts</span>
    <h1>User <em>management</em></h1>
    <p>
      Grants, resets, and recent users.{#if data.degraded}
        <em> Live user data unavailable; showing sample.</em>{/if}
    </p>
  </div>

  <div class="stat-grid">
    <StatTile icon="users" label="Registered" value={data.stats.total_users.toLocaleString()} unit="total" delta={`${data.stats.active_users} active`} flat />
    <StatTile icon="pulse" tan label="Premium" value={data.stats.premium_users.toLocaleString()} unit="" delta="paid + vip" flat />
    <StatTile icon="check" label="VIP" value={data.stats.vip_users.toLocaleString()} unit="" delta="comped" flat />
    <StatTile icon="commands" tan label="Paid" value={data.stats.paid_users.toLocaleString()} unit="" delta="subscribers" flat />
  </div>

  <div class="card">
    <div class="card-head"><h3>Lookup</h3></div>
    <form method="POST" action="?/lookup" use:enhance style="display:flex;gap:.6rem;flex-wrap:wrap">
      <label class="search" style="flex:1;min-width:0">
        <Icon name="search" size={15} />
        <input name="q" type="text" placeholder="Twitch user id or username" autocomplete="off" />
      </label>
      <Button variant="primary" icon="search" type="submit">Look up</Button>
    </form>

    {#if lookupError}
      <p class="notice-muted" style="margin-top:.8rem">{lookupError}</p>
    {/if}

    {#if found}
      <div class="card user-card" style="margin-top:1rem">
        <div class="card-head">
          <h3>@{found.username}</h3>
          <span class="more">id {found.id} · {tier(found.status)}</span>
        </div>
        <div class="meta-row">
          <span>status <b>{found.status}</b></span>
          <span class="sep">·</span>
          <span>{found.is_active ? 'active' : 'inactive'}</span>
          <span class="sep">·</span>
          <span>token {tokenPresent ? 'present' : 'absent'}</span>
        </div>

        {#if action}
          <p class="notice-{action.ok ? 'ok' : 'err'}" style="margin-bottom:.6rem">{action.notice}</p>
        {/if}

        <div class="actions-wrap">
          {#each ['free', 'paid', 'vip'] as s}
            <form method="POST" action="?/setStatus" use:enhance>
              <input type="hidden" name="user_id" value={found.id} />
              <input type="hidden" name="status" value={s} />
              <button class="btn ghost" type="submit" disabled={found.status === s}>Set {s}</button>
            </form>
          {/each}
          <form method="POST" action="?/reset" use:enhance>
            <input type="hidden" name="user_id" value={found.id} />
            <button class="btn ghost" type="submit">Reset</button>
          </form>
          <form method="POST" action="?/clearToken" use:enhance>
            <input type="hidden" name="user_id" value={found.id} />
            <button class="btn ghost" type="submit">Clear token</button>
          </form>
          <button class="btn danger" type="button" onclick={() => openDelete({ id: found.id, username: found.username })}>
            <Icon name="trash" size={13} /> Delete
          </button>
        </div>
      </div>
    {/if}
  </div>

  <div class="card" style="padding:18px 6px">
    <div class="card-head" style="padding:0 12px"><h3>Recent</h3></div>
    <div class="table">
      <div class="thead">
        <span>User</span><span>Id</span><span class="perm-cell">Tier</span><span>Status</span><span>Active</span><span></span>
      </div>
      <div class="trows">
        {#if data.recent.length === 0}
          <div class="trow"><span class="resp" style="grid-column:1/-1;opacity:.6">No recent users.</span></div>
        {/if}
        {#each data.recent as u (u.id)}
          <div class="trow">
            <span class="cmd">@{u.username}</span>
            <span class="resp">{u.id}</span>
            <span class="perm-cell"><span class="badge {tier(u.status) === 'premium' ? 'sub' : 'everyone'}">{tier(u.status)}</span></span>
            <span class="cd">{u.status}</span>
            <span class="uses">{u.is_active ? 'yes' : 'no'}</span>
            <span class="row-act">
              <button
                class="mini danger-mini"
                type="button"
                aria-label="Delete {u.username}"
                title="Delete user"
                onclick={() => openDelete({ id: u.id, username: u.username })}
              >
                <Icon name="trash" size={14} />
              </button>
            </span>
          </div>
        {/each}
      </div>
    </div>
  </div>
</section>

<!-- Delete confirm modal -->
{#if deleteTarget}
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div class="modal-backdrop" onclick={closeDelete} role="dialog" aria-modal="true" aria-labelledby="del-title" tabindex="-1">
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <div class="modal-card" role="presentation" onclick={(e) => e.stopPropagation()}>
      <h3 id="del-title">Delete @{deleteTarget.username}?</h3>
      <p class="modal-body">
        This permanently removes the user and cascades to their commands and modules. This cannot be undone.
      </p>
      <form
        method="POST"
        action="?/delete"
        use:enhance
        bind:this={deleteFormEl}
        style="display:none"
      >
        <input type="hidden" name="user_id" value={deleteTarget.id} />
      </form>
      <div class="modal-actions">
        <button class="btn ghost" type="button" onclick={closeDelete}>Cancel</button>
        <button class="btn danger" type="button" onclick={confirmDelete}>
          <Icon name="trash" size={13} /> Delete permanently
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  /* notice text variants */
  .notice-muted { font-size: .85rem; color: var(--bb-muted); margin: 0; }
  .notice-ok  { font-size: .82rem; color: var(--bb-green-glow); margin: 0; }
  .notice-err { font-size: .82rem; color: #cf8a78; margin: 0; }

  /* user card meta row */
  .meta-row {
    display: flex;
    flex-wrap: wrap;
    gap: .3rem .6rem;
    font-family: var(--bb-font-mono);
    font-size: 12px;
    color: var(--bb-muted);
    margin-bottom: .8rem;
  }
  .meta-row b { color: var(--bb-tan-light); }
  .meta-row .sep { color: var(--bb-border-strong); }

  /* action row in user card */
  .actions-wrap {
    display: flex;
    flex-wrap: wrap;
    gap: .5rem;
    align-items: center;
  }
  .actions-wrap form { display: contents; }

  /* danger button variant */
  .btn.danger {
    background: rgba(176, 90, 70, 0.12);
    color: #cf8a78;
    border-color: rgba(176, 90, 70, 0.35);
  }
  .btn.danger:hover {
    background: rgba(176, 90, 70, 0.22);
    color: #e09e8a;
    border-color: rgba(176, 90, 70, 0.55);
  }
  .btn.danger:disabled { opacity: .45; cursor: not-allowed; }

  /* danger mini button (table row) */
  .danger-mini { color: rgba(176, 90, 70, 0.7); }
  .danger-mini:hover { color: #cf8a78 !important; background: rgba(176, 90, 70, 0.10) !important; }

  /* confirm modal */
  .modal-backdrop {
    position: fixed;
    inset: 0;
    z-index: 200;
    background: rgba(0, 0, 0, 0.55);
    backdrop-filter: blur(4px);
    -webkit-backdrop-filter: blur(4px);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 16px;
  }

  .modal-card {
    background: var(--bb-bg-1, #111);
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-lg);
    backdrop-filter: blur(var(--glass-blur));
    -webkit-backdrop-filter: blur(var(--glass-blur));
    padding: 28px 28px 24px;
    max-width: 420px;
    width: 100%;
  }

  .modal-card h3 {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 19px;
    color: var(--bb-white);
    margin: 0 0 12px;
    letter-spacing: -0.01em;
  }

  .modal-body {
    font-family: var(--bb-font-body);
    font-size: 14px;
    color: var(--bb-muted);
    line-height: 1.55;
    margin: 0 0 22px;
  }

  .modal-actions {
    display: flex;
    gap: .6rem;
    justify-content: flex-end;
    flex-wrap: wrap;
  }

  /* mobile responsive */
  @media (max-width: 760px) {
    .user-card .actions-wrap {
      gap: .4rem;
    }
    /* stat-grid already goes 2-col via shared app.css; ensure it for this page too */
    :global(.stat-grid) {
      grid-template-columns: 1fr 1fr;
    }
  }

  @media (max-width: 380px) {
    .modal-card { padding: 20px 16px 18px; }
    .actions-wrap { gap: .35rem; }
    .btn { font-size: 10px; padding: 10px 14px; }
  }
</style>
