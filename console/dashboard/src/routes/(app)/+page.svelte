<script lang="ts">
  import { enhance } from '$app/forms';
  import { onMount } from 'svelte';
  import { page } from '$app/state';
  import { Button, Card, CardHead, Icon, PageHead, StatTile, Modal, Skeleton, getI18n, type IconName } from '@bagel/shared';
  import OnboardingModal from '$lib/components/OnboardingModal.svelte';
  let { data } = $props();

  const { t } = getI18n();

  // First-visit onboarding: opens once for genuinely new users (nothing
  // created yet, never dismissed) or on demand via ?welcome=1.
  let onboardOpen = $state(false);
  let onboardForm: HTMLFormElement;

  onMount(() => {
    if (page.url.searchParams.get('welcome') === '1') {
      onboardOpen = true;
      return;
    }
    if (data.onboarded) return;
    
    // Only open the modal if the user actually has no commands (and isn't onboarded yet)
    data.commands.then((cd) => {
      if (cd.total === 0) onboardOpen = true;
    });
  });

  function finishOnboarding() {
    onboardOpen = false;
    onboardForm?.requestSubmit();
  }

  // Real problems only, each with its fix. Empty array = healthy.
  type Issue = { icon: IconName; text: string; cta: string; href: string | null };
  function issuesFor(
    c: { enabled: boolean; receiving: boolean },
    ss: string,
    commandTotal: number
  ): Issue[] {
    const out: Issue[] = [];
    if (!c.enabled) out.push({ icon: 'power', text: t('overview.issueNoAuth'), cta: t('overview.issueNoAuthCta'), href: '/settings' });
    if (c.enabled && !c.receiving) out.push({ icon: 'activity', text: t('overview.issueIdle'), cta: t('overview.issueIdleCta'), href: null });
    if (ss === 'failing') out.push({ icon: 'ban', text: t('overview.issueSubs'), cta: t('overview.issueSubsCta'), href: '/settings' });
    if (commandTotal === 0) out.push({ icon: 'commands', text: t('overview.issueNoCommands'), cta: t('overview.issueNoCommandsCta'), href: '/commands' });
    return out;
  }

  const statusLabel = (s: string) =>
    t(`planLabel.${(['free', 'paid', 'vip'].includes(s) ? s : 'free')}`);

  let greeting = $state(t('overview.greetingEvening'));

  function greetingForHour(hour: number): string {
    if (hour >= 5 && hour < 12) return t('overview.greetingMorning');
    if (hour >= 12 && hour < 17) return t('overview.greetingAfternoon');
    return t('overview.greetingEvening');
  }

  onMount(() => {
    greeting = greetingForHour(new Date().getHours());
  });

  // Confirm modal state
  type PendingAction = 'restart' | 'disconnect' | null;
  let pending = $state<PendingAction>(null);

  const modalTitle = $derived(
    pending === 'restart' ? t('overview.modalRestartTitle') : t('overview.modalDisconnectTitle')
  );
  const modalBody = $derived(
    pending === 'restart' ? t('overview.modalRestartBody') : t('overview.modalDisconnectBody')
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
  <PageHead eyebrow={t('overview.eyebrow')} description={t('overview.description')}>{greeting}, <em>{data.displayName ?? data.login}</em></PageHead>

  <!-- status-hero keeps page-scoped descendant styles (.live.off/.meta/.botmark),
       so it stays a raw glass card rather than the <Card> component. -->
  <div class="card sheen status-hero" class:status-hero--premium={data.isPremium}>
    <div class="botmark"><img src={data.isPremium ? '/premium-logo.png' : '/logo.png'} alt="" /></div>
    <!-- Connection state streams in after the shell renders; show a neutral
         placeholder until the RPC lands so navigation stays instant. -->
    {#await data.conn}
      <div>
        <div class="live off"><span class="dot"></span> {t('overview.checking')}</div>
        <div class="meta"><Skeleton variant="pill" /></div>
      </div>
      <div class="actions"></div>
    {:then c}
      {@const ss = sub?.state ?? c.subState}
      <div>
        <div class="live {c.receiving ? '' : 'off'}">
          <span class="dot"></span> {c.receiving ? t('overview.onlineInChat') : c.enabled ? t('overview.connectedIdle') : t('overview.notConnected')}
        </div>
        <div class="meta">
          <span class="status-tag {c.status !== 'free' ? 'premium' : ''}">{statusLabel(c.status)}</span>
          {#if ss === 'failing'}
            <span class="status-tag sub-state err">{t('overview.reconnectNeeded')}</span>
          {:else if ss === 'pending'}
            <span class="status-tag sub-state warn">{t('overview.reconnecting')}</span>
          {/if}
        </div>
        {#if ss === 'failing'}
          <p class="sub-fix">{t('overview.subFixPre')}<strong>{t('overview.subFixStrong')}</strong>.</p>
        {/if}
      </div>
      <div class="actions">
        {#if c.receiving}
          <Button variant="ghost" icon="activity" type="button" onclick={() => openModal('restart')}>{t('overview.restart')}</Button>
          <Button variant="tan" icon="power" type="button" onclick={() => openModal('disconnect')}>{t('overview.disconnect')}</Button>
        {:else}
          <form method="POST" action="?/enable" use:enhance={enableSubmit}>
            <Button variant="primary" icon="power" type="submit">{t('overview.enable')}</Button>
          </form>
        {/if}
      </div>
    {/await}
  </div>

  <!-- Quick actions: the three things a streamer actually comes here to do. -->
  <div class="quick-row">
    <a class="btn primary" href="/commands"><Icon name="plus" size={14} /> {t('overview.quickNewCommand')}</a>
    <a class="btn ghost" href="/modules"><Icon name="modules" size={14} /> {t('overview.quickModules')}</a>
    <a class="btn ghost" href="/settings"><Icon name="settings" size={14} /> {t('overview.quickSettings')}</a>
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
        <p class="all-good"><Icon name="check" size={13} /> {t('overview.allGood')}</p>
      {/if}
    {/await}
  {/await}

  <!-- At a glance: your bot's actual numbers, each linking to its page. -->
  <div class="stat-grid overview-stats">
    {#await data.commands}
      <StatTile icon="commands" label={t('overview.statActiveCommands')} value="—" delta={t('overview.counting')} flat />
    {:then cd}
      <StatTile
        icon="commands"
        label={t('overview.statActiveCommands')}
        value={String(cd.active)}
        unit={t('overview.ofN', { n: cd.total })}
        delta={cd.uses > 0 ? t('overview.usesAllTime', { n: cd.uses.toLocaleString() }) : t('overview.createFirstResponse')}
      />
    {/await}
    {#await data.modules}
      <StatTile icon="modules" tan label={t('overview.statModulesOn')} value="—" delta={t('overview.checkingShort')} flat />
    {:then md}
      <StatTile
        icon="modules"
        tan
        label={t('overview.statModulesOn')}
        value={String(md.on)}
        unit={t('overview.ofN', { n: md.total })}
        delta={md.on > 0 ? t('overview.runningForChannel') : t('overview.browseCatalog')}
      />
    {/await}
    {#await data.shares}
      <StatTile icon="users" label={t('overview.statSharedAccess')} value="—" delta={t('overview.checkingShort')} flat />
    {:then sh}
      <StatTile
        icon="users"
        label={t('overview.statSharedAccess')}
        value={String(sh.people)}
        unit={sh.people === 1 ? t('overview.person') : t('overview.people')}
        delta={sh.pending > 0 ? t('overview.invitesPending', { n: sh.pending }) : t('overview.manageInSettings')}
        flat={sh.pending === 0}
      />
    {/await}
    {#await data.conn}
      <StatTile icon="pulse" tan label={t('overview.statPlan')} value="—" delta={t('overview.loadingAccount')} flat />
    {:then c}
      <StatTile
        icon="pulse"
        tan
        label={t('overview.statPlan')}
        value={statusLabel(c.status)}
        delta={c.status === 'free' ? t('overview.standardAccess') : t('overview.premiumAccess')}
        flat
      />
    {/await}
  </div>

  <div class="overview-grid">
    <Card>
      <CardHead title={t('overview.topCommands')}>{#snippet action()}<a class="more" href="/commands">{t('overview.allCommands')}</a>{/snippet}</CardHead>
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
                <span class="fw uses">{t('overview.usesN', { n: c.uses ?? '0' })}</span>
              </div>
            {/each}
            <div class="feed-row">
              <div class="fi"><Icon name="plus" size={15} /></div>
              <div class="ft">
                <b>{t('overview.addAnother')}</b>
                <span>{t('overview.addAnotherDesc')}</span>
              </div>
              <a class="fw overview-link" href="/commands">{t('common.open')}</a>
            </div>
          </div>
        {:else}
          <div class="feed">
            <div class="feed-row">
              <div class="fi green"><Icon name="commands" size={15} /></div>
              <div class="ft">
                <b>{t('overview.createFirstCommand')}</b>
                <span>{t('overview.createFirstCommandDesc')}</span>
              </div>
              <a class="fw overview-link" href="/commands">{t('common.open')}</a>
            </div>
            <div class="feed-row">
              <div class="fi"><Icon name="settings" size={15} /></div>
              <div class="ft">
                <b>{t('overview.checkAccess')}</b>
                <span>{t('overview.checkAccessDesc')}</span>
              </div>
              <a class="fw overview-link" href="/settings">{t('common.open')}</a>
            </div>
          </div>
        {/if}
      {/await}
    </Card>

  </div>
</section>

<!-- First-visit setup stepper -->
<OnboardingModal open={onboardOpen} onDone={finishOnboarding} />

<form method="POST" action="?/onboarded" use:enhance bind:this={onboardForm} hidden></form>

<!-- Confirm modal -->
<Modal open={pending !== null} title={modalTitle} closeModal={closeModal}>
  {#if pending !== null}
    <p class="modal-body">{modalBody}</p>
    <form method="POST" action={modalAction} use:enhance={closeAfterSubmit} class="modal-actions">
      <Button variant="ghost" type="button" onclick={closeModal}>{t('common.cancel')}</Button>
      <Button
        variant={pending === 'disconnect' ? 'tan' : 'primary'}
        type="submit"
      >
        {pending === 'restart' ? t('overview.restart') : t('overview.disconnect')}
      </Button>
    </form>
  {/if}
</Modal>

<style>
  /* With the #login heading gone, the connection status is the hero's headline:
     promote it from a small mono pill to a display-weight line that fills the row. */
  .status-hero .live {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 22px;
    letter-spacing: -0.01em;
    text-transform: none;
    color: var(--bb-white);
    gap: 12px;
    margin-bottom: 12px;
  }
  
  :global(.status-hero--premium .botmark) {
    border-color: rgba(201, 168, 124, 0.4) !important;
    background: rgba(201, 168, 124, 0.05) !important;
  }
  .status-hero .live .dot { width: 9px; height: 9px; }
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
    border-radius: 8px 8px;
  }
  .attn-ico {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    flex: none;
    border-radius: 8px 8px;
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
