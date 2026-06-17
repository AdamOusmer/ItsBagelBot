<script lang="ts">
  import { enhance } from '$app/forms';
  import { onMount } from 'svelte';
  import { Button, Icon, StatTile } from '@bagel/shared';
  let { data } = $props();

  const statusLabel = (s: string) =>
    ({ free: 'Free', paid: 'Paid', vip: 'VIP' })[s] ?? 'Free';

  type Greeting = 'Good morning' | 'Good afternoon' | 'Good evening';

  let greeting = $state<Greeting>('Good evening');

  function greetingForHour(hour: number): Greeting {
    if (hour >= 5 && hour < 12) return 'Good morning';
    if (hour >= 12 && hour < 17) return 'Good afternoon';
    return 'Good evening';
  }

  onMount(() => {
    greeting = greetingForHour(new Date().getHours());
  });

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
  const modalAction = $derived(pending === 'restart' ? '?/restart' : '?/disconnect');

  function openModal(action: PendingAction) {
    pending = action;
  }

  function closeModal() {
    pending = null;
  }

  function closeAfterSubmit() {
    return async ({ update }: { update: (opts?: { invalidateAll?: boolean }) => Promise<void> }) => {
      await update();
      closeModal();
    };
  }
</script>

<section class="screen active">
  <div class="page-head">
    <span class="eyebrow">Status</span>
    <h1>{greeting}, <em>{data?.displayName ?? 'there'}</em></h1>
    <p>Manage your bot connection and commands from here.</p>
  </div>

  <div class="card sheen status-hero">
    <div class="botmark"><img src="/logo.png" alt="" /></div>
    <!-- Connection state streams in after the shell renders; show a neutral
         placeholder until the RPC lands so navigation stays instant. -->
    {#await data.conn}
      <div>
        <div class="live off"><span class="dot"></span> Checking connection…</div>
        <h2>#{data.login ?? 'itsmavey'}</h2>
        <div class="meta"><span class="status-tag">—</span></div>
      </div>
      <div class="actions"></div>
    {:then c}
      <div>
        <div class="live {c.receiving ? '' : 'off'}">
          <span class="dot"></span> {c.receiving ? 'Online · in chat' : c.enabled ? 'Connected · idle' : 'Not connected'}
        </div>
        <h2>#{data.login ?? 'itsmavey'}</h2>
        <div class="meta">
          <span class="status-tag {c.status !== 'free' ? 'premium' : ''}">{statusLabel(c.status)}</span>
        </div>
      </div>
      <div class="actions">
        {#if c.receiving}
          <Button variant="ghost" icon="activity" type="button" onclick={() => openModal('restart')}>Restart</Button>
          <Button variant="tan" icon="power" type="button" onclick={() => openModal('disconnect')}>Disconnect</Button>
        {:else}
          <form method="POST" action="?/enable" use:enhance>
            <Button variant="primary" icon="power" type="submit">Enable</Button>
          </form>
        {/if}
      </div>
    {/await}
  </div>

  {#await data.conn}
    <div class="stat-grid overview-stats">
      <StatTile icon="power" label="Bot status" value="—" delta="checking connection…" flat />
      <StatTile icon="activity" tan label="Chat delivery" value="—" delta="checking EventSub…" flat />
      <StatTile icon="pulse" label="Plan" value="—" delta="loading account…" flat />
      <StatTile icon="commands" tan label="Commands" value="Open" delta="manage responses" flat />
    </div>
  {:then c}
    <div class="stat-grid overview-stats">
      <StatTile
        icon="power"
        label="Bot status"
        value={c.receiving ? 'Live' : c.enabled ? 'Idle' : 'Off'}
        delta={c.receiving ? 'serving your channel' : c.enabled ? 'connected, not receiving' : 'enable when ready'}
        flat
      />
      <StatTile
        icon="activity"
        tan
        label="Chat delivery"
        value={c.receiving ? 'On' : 'Paused'}
        delta={c.enabled ? 'authorization stored' : 'authorization needed'}
        flat
      />
      <StatTile
        icon="pulse"
        label="Plan"
        value={statusLabel(c.status)}
        delta={c.status === 'free' ? 'standard access' : 'premium access'}
        flat
      />
      <StatTile icon="commands" tan label="Commands" value="Open" delta="review chat responses" flat />
    </div>
  {/await}

  <div class="grid-2 overview-grid">
    <div class="card">
      <div class="card-head">
        <h3>Next up</h3>
        <a class="more" href="/commands">Commands</a>
      </div>
      <div class="feed">
        <div class="feed-row">
          <div class="fi green"><Icon name="commands" size={15} /></div>
          <div class="ft">
            <b>Review chat commands</b>
            <span>Create, edit, disable, and tune cooldowns for your channel responses.</span>
          </div>
          <a class="fw overview-link" href="/commands">Open</a>
        </div>
        <div class="feed-row">
          <div class="fi"><Icon name="settings" size={15} /></div>
          <div class="ft">
            <b>Check account access</b>
            <span>Reconnect Twitch or manage shared dashboard links.</span>
          </div>
          <a class="fw overview-link" href="/settings">Open</a>
        </div>
        <div class="feed-row">
          <div class="fi green"><Icon name="activity" size={15} /></div>
          <div class="ft">
            <b>Keep the bot fresh</b>
            <span>Restart the connection if chat delivery ever feels stale.</span>
          </div>
          <span class="fw">Overview</span>
        </div>
      </div>
    </div>

    <div class="card">
      <div class="card-head"><h3>Connection checklist</h3></div>
      {#await data.conn}
        <div class="node-list">
          <div class="node-row"><span class="nd warn"></span><span class="nm">Twitch grant</span><span class="sv">Checking</span><span class="pg">—</span></div>
          <div class="node-row"><span class="nd warn"></span><span class="nm">Bot active</span><span class="sv">Checking</span><span class="pg">—</span></div>
          <div class="node-row"><span class="nd warn"></span><span class="nm">Account tier</span><span class="sv">Checking</span><span class="pg">—</span></div>
        </div>
      {:then c}
        <div class="node-list">
          <div class="node-row">
            <span class="nd {c.enabled ? '' : 'warn'}"></span>
            <span class="nm">Twitch grant</span>
            <span class="sv">{c.enabled ? 'Authorized' : 'Needs reconnect'}</span>
            <span class="pg">{c.enabled ? 'OK' : 'Set up'}</span>
          </div>
          <div class="node-row">
            <span class="nd {c.receiving ? '' : 'warn'}"></span>
            <span class="nm">Bot active</span>
            <span class="sv">{c.receiving ? 'Receiving events' : c.enabled ? 'Ready to enable' : 'Waiting'}</span>
            <span class="pg">{c.receiving ? 'Live' : 'Paused'}</span>
          </div>
          <div class="node-row">
            <span class="nd"></span>
            <span class="nm">Account tier</span>
            <span class="sv">{statusLabel(c.status)}</span>
            <span class="pg">{c.status === 'free' ? 'Base' : 'Plus'}</span>
          </div>
        </div>
      {/await}
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
    <form method="POST" action={modalAction} use:enhance={closeAfterSubmit} class="modal-actions">
      <button class="btn ghost" type="button" onclick={closeModal}>Cancel</button>
      <button
        class="btn {pending === 'disconnect' ? 'tan' : 'primary'}"
        type="submit"
      >
        {pending === 'restart' ? 'Restart' : 'Disconnect'}
      </button>
    </form>
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
  .overview-stats {
    margin-bottom: var(--row-gap);
  }
  .overview-grid {
    align-items: stretch;
  }
  .overview-link {
    color: var(--bb-tan);
    text-decoration: none;
  }
  .overview-link:hover {
    color: var(--bb-tan-pale);
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
