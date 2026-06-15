<script lang="ts">
  import { enhance } from '$app/forms';
  import { Button } from '@bagel/shared';
  let { data } = $props();

  const statusLabel = $derived({ free: 'Free', paid: 'Paid', vip: 'VIP' }[data.status] ?? 'Free');
  const paid = $derived(data.status !== 'free');

  // Confirm modal state
  type PendingAction = 'restart' | 'disconnect' | null;
  let pending = $state<PendingAction>(null);

  const modalTitle = $derived(
    pending === 'restart' ? 'Restart bot connection?' : 'Disconnect bot?'
  );
  const modalBody = $derived(
    pending === 'restart'
      ? 'This drops all your EventSub subscriptions and immediately reconnects them.'
      : 'This disconnects your bot from chat and drops all active EventSub subscriptions.'
  );

  function openModal(action: PendingAction) {
    pending = action;
  }

  function closeModal() {
    pending = null;
  }

  // Submit the hidden form when user confirms
  function confirm() {
    const id = pending === 'restart' ? 'form-restart' : 'form-disconnect';
    pending = null;
    (document.getElementById(id) as HTMLFormElement | null)?.requestSubmit();
  }
</script>

<section class="screen active">
  <div class="page-head">
    <span class="eyebrow">Status</span>
    <h1>Good evening, <em>{data?.displayName ?? 'there'}</em></h1>
    <p>Manage your bot connection, commands, and modules from here.</p>
  </div>

  <div class="card sheen status-hero">
    <div class="botmark"><img src="/logo.png" alt="" /></div>
    <div>
      <div class="live {data.receiving ? '' : 'off'}">
        <span class="dot"></span> {data.receiving ? 'Online · in chat' : data.enabled ? 'Connected · idle' : 'Not connected'}
      </div>
      <h2>#{data.login ?? 'itsmavey'}</h2>
      <div class="meta">
        <span class="status-tag {paid ? 'premium' : ''}">{statusLabel}</span>
      </div>
    </div>
    <div class="actions">
      {#if data.receiving}
        <!-- Hidden real forms — only submitted after modal confirm -->
        <form id="form-restart" method="POST" action="?/restart" use:enhance style="display:none"></form>
        <form id="form-disconnect" method="POST" action="?/disconnect" use:enhance style="display:none"></form>
        <Button variant="ghost" icon="activity" type="button" onclick={() => openModal('restart')}>Restart</Button>
        <Button variant="tan" icon="power" type="button" onclick={() => openModal('disconnect')}>Disconnect</Button>
      {:else}
        <form method="POST" action="?/enable" use:enhance>
          <Button variant="primary" icon="power" type="submit">Enable</Button>
        </form>
      {/if}
    </div>
  </div>
</section>

<!-- Confirm modal -->
{#if pending !== null}
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div
    class="modal-backdrop"
    role="button"
    tabindex="-1"
    aria-label="Close dialog"
    onclick={closeModal}
  ></div>
  <dialog class="confirm-dialog" open aria-modal="true" aria-labelledby="modal-title">
    <h3 id="modal-title">{modalTitle}</h3>
    <p>{modalBody}</p>
    <div class="modal-actions">
      <button class="btn ghost" onclick={closeModal}>Cancel</button>
      <button
        class="btn {pending === 'disconnect' ? 'tan' : 'primary'}"
        onclick={confirm}
      >
        {pending === 'restart' ? 'Restart' : 'Disconnect'}
      </button>
    </div>
  </dialog>
{/if}

<svelte:window onkeydown={(e) => { if (e.key === 'Escape') closeModal(); }} />

<style>
  .status-hero .live.off { color: var(--bb-muted); }
  .status-hero .live.off .dot { background: var(--bb-muted); box-shadow: none; animation: none; }
  .status-tag {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    padding: 5px 12px;
    border-radius: var(--bb-radius-pill);
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid var(--bb-border);
    color: var(--bb-muted);
  }
  .status-tag.premium {
    background: rgba(82, 183, 136, 0.12);
    border-color: rgba(82, 183, 136, 0.35);
    color: var(--bb-green-glow);
  }

  /* Mobile: stack botmark above text, actions full-width buttons */
  @media (max-width: 760px) {
    :global(.status-hero .actions) {
      flex-direction: column;
    }
    /* Buttons from the shared Button component need full width too */
    :global(.status-hero .actions button),
    :global(.status-hero .actions > button) {
      width: 100%;
      justify-content: center;
      min-height: 44px;
    }
  }

  /* Confirm modal */
  .modal-backdrop {
    position: fixed;
    inset: 0;
    z-index: 200;
    background: rgba(0, 0, 0, 0.55);
    backdrop-filter: blur(4px);
    -webkit-backdrop-filter: blur(4px);
  }

  .confirm-dialog {
    position: fixed;
    inset: 0;
    margin: auto;
    z-index: 201;
    width: min(400px, calc(100vw - 32px));
    height: fit-content;
    background: var(--bb-card-bg);
    border: 1px solid var(--bb-border-strong);
    border-radius: var(--bb-radius-lg);
    padding: 28px 24px 20px;
    box-shadow: 0 24px 64px rgba(0, 0, 0, 0.6);
    /* reset browser <dialog> defaults */
    display: block;
    color: var(--bb-white);
  }

  .confirm-dialog h3 {
    font-family: var(--bb-font-display);
    font-weight: 600;
    font-size: 18px;
    letter-spacing: -0.01em;
    color: var(--bb-white);
    margin: 0 0 10px;
  }

  .confirm-dialog p {
    font-family: var(--bb-font-body);
    font-size: 14px;
    line-height: 1.55;
    color: var(--bb-muted);
    margin: 0 0 22px;
  }

  .modal-actions {
    display: flex;
    gap: 10px;
    justify-content: flex-end;
  }

  @media (max-width: 480px) {
    .modal-actions {
      flex-direction: column-reverse;
    }
    .modal-actions :global(.btn),
    .modal-actions button {
      width: 100%;
      justify-content: center;
      min-height: 44px;
    }
  }
</style>
