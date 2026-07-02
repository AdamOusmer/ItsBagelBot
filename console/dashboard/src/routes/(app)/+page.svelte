<script lang="ts">
  import { enhance } from '$app/forms';
  import { onMount } from 'svelte';
  import { Button, Card, CardHead, Icon, PageHead, StatTile, Modal, Skeleton, type IconName } from '@bagel/shared';
  let { data } = $props();

  // Real problems only, each with its fix. Empty array = healthy.
  type Issue = { icon: IconName; text: string; cta: string; href: string | null };
  function issuesFor(
    c: { enabled: boolean; receiving: boolean },
    ss: string,
    commandTotal: number
  ): Issue[] {
    const out: Issue[] = [];
    if (!c.enabled) out.push({ icon: 'power', text: 'The bot has no Twitch authorization for your channel.', cta: 'Connect', href: '/settings' });
    if (c.enabled && !c.receiving) out.push({ icon: 'activity', text: 'The bot is connected but not replying in chat.', cta: 'Enable above', href: null });
    if (ss === 'failing') out.push({ icon: 'ban', text: 'Chat subscriptions dropped — viewers may not get replies.', cta: 'Reconnect', href: '/settings' });
    if (commandTotal === 0) out.push({ icon: 'commands', text: 'You have no commands yet — the bot has nothing to say.', cta: 'Create one', href: '/commands' });
    return out;
  }

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

  // Live enroll state. The SSR `conn` carries a one-shot snapshot; a reconnect
  // resolves asynchronously in outgress, so we poll /substate to flip the pill
  // from "reconnecting" to ok/failing without a manual refresh. `sub` (when set)
  // overrides the server snapshot.
  let sub = $state<{ state: string; error: string } | null>(null);
  let pollTimer: ReturnType<typeof setInterval> | null = null;

  function stopPolling() {
    if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  async function refreshSub(): Promise<string> {
    try {
      const r = await fetch('/substate');
      if (r.ok) {
        sub = await r.json();
        return sub?.state ?? 'unknown';
      }
    } catch {
      /* transient; the next tick retries */
    }
    return 'unknown';
  }

  // Poll while pending, backstopped at ~30s so a stuck job stops spinning.
  function startPolling() {
    stopPolling();
    let ticks = 0;
    pollTimer = setInterval(async () => {
      ticks += 1;
      const state = await refreshSub();
      if (state !== 'pending' || ticks >= 12) stopPolling();
    }, 2500);
  }

  // Mark reconnecting immediately on user action, then poll to the outcome.
  function trackReconnect() {
    sub = { state: 'pending', error: '' };
    startPolling();
  }

  onMount(() => {
    refreshSub().then((state) => {
      if (state === 'pending') startPolling();
    });
    return stopPolling;
  });

  function closeAfterSubmit() {
    const wasRestart = pending === 'restart';
    return async ({ update }: { update: (opts?: { invalidateAll?: boolean }) => Promise<void> }) => {
      await update();
      closeModal();
      // Only a restart re-enrolls; disconnect just tears down.
      if (wasRestart) trackReconnect();
    };
  }

  function enableSubmit() {
    return async ({ update }: { update: (opts?: { invalidateAll?: boolean }) => Promise<void> }) => {
      await update();
      trackReconnect();
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
        <div class="meta"><Skeleton variant="pill" /></div>
      </div>
      <div class="actions"></div>
    {:then c}
      {@const ss = sub?.state ?? c.subState}
      <div>
        <div class="live {c.receiving ? '' : 'off'}">
          <span class="dot"></span> {c.receiving ? 'Online · in chat' : c.enabled ? 'Connected · idle' : 'Not connected'}
        </div>
        <h2>#{data.login ?? 'itsmavey'}</h2>
        <div class="meta">
          <span class="status-tag {c.status !== 'free' ? 'premium' : ''}">{statusLabel(c.status)}</span>
          {#if ss === 'failing'}
            <span class="status-tag sub-state err">Reconnect needed</span>
          {:else if ss === 'pending'}
            <span class="status-tag sub-state warn">Reconnecting…</span>
          {/if}
        </div>
        {#if ss === 'failing'}
          <p class="sub-fix">Chat subscriptions dropped. Fix in <strong>Settings → Reconnect</strong>.</p>
        {/if}
      </div>
      <div class="actions">
        {#if c.receiving}
          <Button variant="ghost" icon="activity" type="button" onclick={() => openModal('restart')}>Restart</Button>
          <Button variant="tan" icon="power" type="button" onclick={() => openModal('disconnect')}>Disconnect</Button>
        {:else}
          <form method="POST" action="?/enable" use:enhance={enableSubmit}>
            <Button variant="primary" icon="power" type="submit">Enable</Button>
          </form>
        {/if}
      </div>
    {/await}
  </div>

  <!-- Quick actions: the three things a streamer actually comes here to do. -->
  <div class="quick-row">
    <a class="btn primary" href="/commands"><Icon name="plus" size={14} /> New command</a>
    <a class="btn ghost" href="/modules"><Icon name="power" size={14} /> Manage modules</a>
    <a class="btn ghost" href="/settings"><Icon name="settings" size={14} /> Settings</a>
  </div>

  <!-- Needs-attention strip: shows ONLY real problems with their fix; one quiet
       line when everything is healthy. The hero already says "connected", so
       nothing here repeats it. -->
  {#await data.conn then c}
    {@const ss = sub?.state ?? c.subState}
    {#await data.commands then cd}
      {@const issues = issuesFor(c, ss, cd.total)}
      {#if issues.length}
        <div class="attention">
          {#each issues as issue (issue.text)}
            <div class="attn-row">
              <span class="attn-ico"><Icon name={issue.icon} size={14} /></span>
              <span class="attn-text">{issue.text}</span>
              {#if issue.href}<a class="btn ghost sm-btn" href={issue.href}>{issue.cta}</a>
              {:else}<span class="attn-hint">{issue.cta}</span>{/if}
            </div>
          {/each}
        </div>
      {:else}
        <p class="all-good"><Icon name="check" size={13} /> Everything's running — nothing needs you right now.</p>
      {/if}
    {/await}
  {/await}

  <!-- At a glance: your bot's actual numbers, each linking to its page. -->
  <div class="stat-grid overview-stats">
    {#await data.commands}
      <StatTile icon="commands" label="Active commands" value="—" delta="counting…" flat />
    {:then cd}
      <StatTile
        icon="commands"
        label="Active commands"
        value={String(cd.active)}
        unit={`of ${cd.total}`}
        delta={cd.uses > 0 ? `${cd.uses.toLocaleString()} uses all-time` : 'create your first response'}
      />
    {/await}
    {#await data.modules}
      <StatTile icon="power" tan label="Modules on" value="—" delta="checking…" flat />
    {:then md}
      <StatTile
        icon="power"
        tan
        label="Modules on"
        value={String(md.on)}
        unit={`of ${md.total}`}
        delta={md.on > 0 ? 'running for your channel' : 'browse the catalog'}
      />
    {/await}
    {#await data.shares}
      <StatTile icon="users" label="Shared access" value="—" delta="checking…" flat />
    {:then sh}
      <StatTile
        icon="users"
        label="Shared access"
        value={String(sh.people)}
        unit={sh.people === 1 ? 'person' : 'people'}
        delta={sh.pending > 0 ? `${sh.pending} invite${sh.pending === 1 ? '' : 's'} pending` : 'manage in Settings'}
        flat={sh.pending === 0}
      />
    {/await}
    {#await data.conn}
      <StatTile icon="pulse" tan label="Plan" value="—" delta="loading account…" flat />
    {:then c}
      <StatTile
        icon="pulse"
        tan
        label="Plan"
        value={statusLabel(c.status)}
        delta={c.status === 'free' ? 'standard access' : 'premium access'}
        flat
      />
    {/await}
  </div>

  <div class="overview-grid">
    <Card>
      <CardHead title="Your top commands">{#snippet action()}<a class="more" href="/commands">All commands</a>{/snippet}</CardHead>
      {#await data.commands}
        <div class="feed">
          {#each [0, 1, 2] as i (i)}
            <div class="feed-row">
              <div class="fi green"><Icon name="commands" size={15} /></div>
              <div class="ft"><Skeleton variant="text" lines={2} width="80%" /></div>
            </div>
          {/each}
        </div>
      {:then cd}
        {@const top = cd.top}
        {#if top.length}
          <div class="feed">
            {#each top as c (c.name)}
              <div class="feed-row">
                <div class="fi green"><Icon name="commands" size={15} /></div>
                <div class="ft">
                  <b class="mono">!{c.name}</b>
                  <span class="clip">{c.response}</span>
                </div>
                <span class="fw uses">{c.uses ?? '0'} uses</span>
              </div>
            {/each}
            <div class="feed-row">
              <div class="fi"><Icon name="plus" size={15} /></div>
              <div class="ft">
                <b>Add another</b>
                <span>Create, edit, and tune cooldowns for your channel responses.</span>
              </div>
              <a class="fw overview-link" href="/commands">Open</a>
            </div>
          </div>
        {:else}
          <div class="feed">
            <div class="feed-row">
              <div class="fi green"><Icon name="commands" size={15} /></div>
              <div class="ft">
                <b>Create your first command</b>
                <span>Custom responses your viewers trigger with !name in chat.</span>
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
          </div>
        {/if}
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
  .sub-fix {
    margin: 8px 0 0;
    max-width: 36ch;
    font-size: 12px;
    line-height: 1.4;
    color: var(--bb-muted);
  }
  .sub-fix strong {
    color: #cf8a78;
    font-weight: 600;
    white-space: nowrap;
  }
  .overview-stats {
    margin-bottom: var(--row-gap);
  }
  .quick-row {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
    margin-bottom: var(--row-gap);
  }
  .quick-row .btn { text-decoration: none; }
  @media (max-width: 480px) {
    .quick-row .btn { flex: 1; justify-content: center; }
  }

  /* needs-attention strip */
  .attention {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin-bottom: var(--row-gap);
  }
  .attn-row {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 16px;
    background: rgba(201, 168, 124, 0.07);
    border: 1px solid rgba(201, 168, 124, 0.3);
    border-radius: var(--bb-radius-md, 10px);
  }
  .attn-ico {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    flex: none;
    border-radius: var(--bb-radius-sm, 6px);
    background: rgba(201, 168, 124, 0.14);
    color: var(--bb-tan-light);
  }
  .attn-ico :global(svg) { stroke: currentColor; fill: none; stroke-width: 1.7; }
  .attn-text {
    flex: 1;
    min-width: 0;
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    color: var(--bb-white);
  }
  .attn-hint { font-family: var(--bb-font-body); font-weight: 600; font-size: 12.5px; color: var(--bb-tan-light); white-space: nowrap; }
  .sm-btn { padding: 7px 14px; font-size: 12px; }

  .all-good {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    margin: 0 0 var(--row-gap);
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-green-glow);
  }
  .all-good :global(svg) { stroke: currentColor; fill: none; stroke-width: 2; }
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
  .mono { font-family: var(--bb-font-mono); }
  .uses {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
    white-space: nowrap;
  }
  .clip {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    display: block;
    max-width: 100%;
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
