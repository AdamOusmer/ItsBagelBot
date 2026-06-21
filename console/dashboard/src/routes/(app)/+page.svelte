<script lang="ts">
  import { enhance } from '$app/forms';
  import { onMount } from 'svelte';
  import { Button, Card, CardHead, Icon, PageHead, StatTile, Modal } from '@bagel/shared';
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
  <PageHead eyebrow="Status" description="Manage your bot connection and commands from here.">{greeting}, <em>{data?.displayName ?? 'there'}</em></PageHead>

  <!-- status-hero keeps page-scoped descendant styles (.live.off/.meta/.botmark),
       so it stays a raw glass card rather than the <Card> component. -->
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
          {#if c.subState === 'failing'}
            <span class="status-tag sub-state err" title={c.subError || 'EventSub enroll failed'}>EventSub failing{c.subError ? ': ' + c.subError.slice(0, 60) : ''}</span>
          {:else if c.subState === 'pending'}
            <span class="status-tag sub-state warn">reconnecting…</span>
          {/if}
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
    <Card>
      <CardHead title="Next up">{#snippet action()}<a class="more" href="/commands">Commands</a>{/snippet}</CardHead>
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
    </Card>

    <Card>
      <CardHead title="Connection checklist"/>
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
    </Card>
  </div>
</section>

<!-- Confirm modal -->
<Modal open={pending !== null} title={modalTitle} closeModal={closeModal}>
  {#if pending !== null}
    <p class="modal-body">{modalBody}</p>
    <form method="POST" action={modalAction} use:enhance={closeAfterSubmit} class="modal-actions">
      <Button variant="ghost" type="button" onclick={closeModal}>Cancel</Button>
      <Button
        variant={pending === 'disconnect' ? 'tan' : 'primary'}
        type="submit"
      >
        {pending === 'restart' ? 'Restart' : 'Disconnect'}
      </Button>
    </form>
  {/if}
</Modal>

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
  .status-tag.sub-state.err {
    background: rgba(176, 90, 70, 0.15);
    border-color: rgba(176, 90, 70, 0.4);
    color: #cf8a78;
  }
  .status-tag.sub-state.warn {
    background: rgba(200, 160, 80, 0.12);
    border-color: rgba(200, 160, 80, 0.35);
    color: var(--bb-tan-light, #c8a050);
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




</style>
