<script lang="ts">
  import { Icon } from '@bagel/shared';
  import { page } from '$app/state';
  import { enhance } from '$app/forms';
  import CheckButton from '$lib/components/CheckButton.svelte';
  import type { DelegationGrant } from '$lib/server/rpc';

  let { data, form } = $props();

  const createdGrant = $derived(form?.createdGrant as DelegationGrant | undefined);
  const given = $derived.by<DelegationGrant[]>(() => {
    const grants = (data.given ?? []) as DelegationGrant[];
    if (!createdGrant || grants.some((g) => g.token === createdGrant.token)) return grants;
    return [createdGrant, ...grants];
  });
  const received = $derived(
    (data.received ?? []) as { owner_user_id: string; owner_login: string; sections: string[] }[]
  );
  const origin = $derived(page.url.origin);

  function linkFor(token: string): string {
    return `${origin}/delegate/accept?t=${token}`;
  }

  async function copy(token: string) {
    try {
      await navigator.clipboard.writeText(linkFor(token));
    } catch {
      /* clipboard blocked; user can select manually */
    }
  }

  // Delete confirm modal: the box must be checked before Delete enables.
  let deleteOpen = $state(false);
  let ack = $state(false);
  const openDelete = () => {
    ack = false;
    deleteOpen = true;
  };
  const closeDelete = () => (deleteOpen = false);
</script>

<section class="screen active">
  <div class="page-head">
    <span class="eyebrow">Account</span>
    <h1>Your <em>settings</em></h1>
    <p>Manage your connection, account, and who can reach parts of your dashboard.</p>
  </div>

  {#if form?.error}
    <p class="banner err">{form.error}</p>
  {:else if form?.ok && form.action === 'created'}
    <p class="banner ok">Link created. Copy it from the list below.</p>
  {:else if form?.ok && form.action === 'revoked'}
    <p class="banner ok">Link revoked.</p>
  {:else if form?.ok && form.action === 'opted_out'}
    <p class="banner ok">Dashboard removed.</p>
  {/if}

  <!-- ACCOUNT -->
  <div class="card">
    <h2>Account</h2>
    <div class="row">
      <div>
        <b>Reconnect Twitch</b>
        <p class="hint">Re-run Twitch authorization to refresh the bot's access to your channel.</p>
      </div>
      <a class="btn ghost" href="/auth/login"><Icon name="power" size={14} /> Reconnect</a>
    </div>
    <div class="row">
      <div>
        <b>Delete account</b>
        <p class="hint">Permanently remove your account and all of your configurations.</p>
      </div>
      <button type="button" class="btn ghost danger" onclick={openDelete}>Delete account</button>
    </div>
  </div>

  <!-- CONTROL -->
  <div class="card">
    <h2>Access you granted</h2>
    <p class="hint">Generate a link to give someone scoped access to your dashboard. The first person to accept it is bound to that access permanently — revoke it here any time.</p>
    {#if given.length === 0}
      <p class="hint">No links yet.</p>
    {:else}
      <table>
        <thead>
          <tr><th>Sections</th><th>Status</th><th>Link</th><th></th></tr>
        </thead>
        <tbody>
          {#each given as g (g.token)}
            <tr>
              <td>{g.sections.join(', ')}</td>
              <td>
                {#if g.consumed}
                  <span class="tag used">Active · {g.delegate_login || 'unknown'}</span>
                {:else}
                  <span class="tag open">Pending · not yet accepted</span>
                {/if}
              </td>
              <td class="linkcell">
                {#if g.consumed}
                  <span class="muted">—</span>
                {:else}
                  <code>{linkFor(g.token)}</code>
                  <button type="button" class="btn ghost sm" onclick={() => copy(g.token)}>Copy</button>
                {/if}
              </td>
              <td>
                <form method="POST" action="?/revoke" use:enhance>
                  <input type="hidden" name="token" value={g.token} />
                  <button type="submit" class="btn ghost sm danger">Revoke</button>
                </form>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}

    <form method="POST" action="?/create" class="create" use:enhance>
      <h3>New share link</h3>
      <p class="hint">Pick which sections the invitee can manage.</p>
      <CheckButton name="commands" checked={true} label="Commands" />
      <button class="btn primary" type="submit"><Icon name="link" size={14} /> Generate link</button>
    </form>
  </div>

  <div class="card">
    <h2>Dashboards shared with you</h2>
    {#if received.length === 0}
      <p class="hint">No one has shared a dashboard with you.</p>
    {:else}
      <table>
        <thead>
          <tr><th>Owner</th><th>Sections</th><th></th></tr>
        </thead>
        <tbody>
          {#each received as r (r.owner_user_id)}
            <tr>
              <td>{r.owner_login}</td>
              <td>{r.sections.join(', ')}</td>
              <td class="actions">
                <a class="btn ghost sm" href={`/delegate/enter?owner=${r.owner_user_id}`}>Open</a>
                <form method="POST" action="?/optOut" use:enhance>
                  <input type="hidden" name="owner_user_id" value={r.owner_user_id} />
                  <button type="submit" class="btn ghost sm danger">Leave</button>
                </form>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</section>

<!-- Delete confirm modal -->
{#if deleteOpen}
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div class="modal-backdrop" role="button" tabindex="-1" aria-label="Close dialog" onclick={closeDelete}></div>
  <dialog class="confirm-dialog" open aria-modal="true" aria-labelledby="del-modal-title">
    <h3 id="del-modal-title">Delete your account?</h3>
    <p>This removes your account, commands, and any links you created. It cannot be undone.</p>
    <div class="ack">
      <CheckButton bind:checked={ack} label="I recognize that this action is irreversible and will lose all my configurations" />
    </div>
    <form method="POST" action="?/delete" use:enhance class="modal-actions">
      <button type="button" class="btn ghost" onclick={closeDelete}>Cancel</button>
      <button type="submit" class="btn delete-btn" disabled={!ack}>Delete account</button>
    </form>
  </dialog>
{/if}

<svelte:window onkeydown={(e) => { if (e.key === 'Escape') closeDelete(); }} />

<style>
  .card {
    background: var(--bb-bg-2, #1a1714);
    border: 1px solid var(--bb-line, rgba(255, 255, 255, 0.08));
    border-radius: var(--bb-radius, 14px);
    padding: 20px;
    margin-top: 18px;
  }
  .card h2 { margin: 0 0 6px; font-size: 16px; }
  .card h3 { margin: 0 0 6px; font-size: 14px; }
  .hint { color: var(--bb-muted, #998f82); font-size: 13px; margin: 0 0 12px; }
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 12px 0;
    border-top: 1px solid var(--bb-line, rgba(255, 255, 255, 0.06));
  }
  .row:first-of-type { border-top: none; }
  .row b { font-size: 14px; }
  .row .hint { margin: 4px 0 0; }
  .create { margin-top: 18px; padding-top: 16px; border-top: 1px solid var(--bb-line, rgba(255, 255, 255, 0.06)); }
  .create .btn { margin-top: 14px; }
  table { width: 100%; border-collapse: collapse; font-size: 13px; }
  th, td { text-align: left; padding: 8px 10px; border-bottom: 1px solid var(--bb-line, rgba(255, 255, 255, 0.06)); }
  th { color: var(--bb-muted, #998f82); font-weight: 600; }
  .linkcell code { font-size: 12px; word-break: break-all; }
  .muted { color: var(--bb-muted, #998f82); }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
  .actions form { margin: 0; }
  .tag { padding: 2px 8px; border-radius: 999px; font-size: 12px; }
  .tag.open { background: rgba(120, 200, 120, 0.18); color: #8fd08f; }
  .tag.used { background: rgba(200, 160, 120, 0.18); color: #c9a87c; }
  .btn.sm { padding: 4px 10px; font-size: 12px; margin-left: 8px; }
  .btn.danger { color: #e08f8f; }
  .banner { padding: 10px 14px; border-radius: 10px; font-size: 13px; margin-top: 14px; }
  .banner.err { background: rgba(220, 120, 120, 0.16); color: #e08f8f; }
  .banner.ok { background: rgba(120, 200, 120, 0.16); color: #8fd08f; }

  /* Modal chrome (mirrors commands page) */
  .modal-backdrop {
    position: fixed;
    inset: 0;
    z-index: 400;
    background: rgba(0, 0, 0, 0.55);
  }
  .confirm-dialog {
    position: fixed;
    z-index: 401;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    width: min(460px, calc(100vw - 32px));
    background: var(--bb-bg-2, #1a1714);
    border: 1px solid var(--bb-line, rgba(255, 255, 255, 0.1));
    border-radius: var(--bb-radius, 14px);
    padding: 22px;
    color: var(--bb-text, #e8e0d6);
  }
  .confirm-dialog h3 { margin: 0 0 10px; font-size: 17px; }
  .confirm-dialog p { color: var(--bb-muted, #998f82); font-size: 13px; margin: 0 0 16px; }
  .ack { margin-bottom: 18px; }
  .modal-actions { display: flex; justify-content: flex-end; gap: 10px; }
  .delete-btn {
    background: rgba(220, 120, 120, 0.16);
    color: #e08f8f;
    border: 1px solid rgba(220, 120, 120, 0.4);
  }
  .delete-btn:disabled { opacity: 0.45; cursor: not-allowed; }

  @media (max-width: 760px) {
    .row { flex-direction: column; align-items: stretch; }
    .actions { justify-content: flex-start; }
    .modal-actions { flex-direction: column-reverse; }
  }
</style>
