<script lang="ts">
  import { enhance } from '$app/forms';
  import { onMount } from 'svelte';
  import { page } from '$app/state';
  import { Button, Card, ButtonLink, Modal, Skeleton, getI18n, connectionUiState, toast, type ConnSignals, type ConnUi } from '@bagel/shared';
  import type { ActionResult } from '@sveltejs/kit';
  import OnboardingModal from '$lib/components/OnboardingModal.svelte';
  import OverviewHead from '$lib/components/overview/OverviewHead.svelte';
  import BotStatusPanel from '$lib/components/overview/BotStatusPanel.svelte';
  import NeedsAttention from '$lib/components/overview/NeedsAttention.svelte';
  import QuickActions from '$lib/components/overview/QuickActions.svelte';
  import LinkedSummary from '$lib/components/overview/LinkedSummary.svelte';
  import TopCommands from '$lib/components/overview/TopCommands.svelte';
  import SetupProgress from '$lib/components/overview/SetupProgress.svelte';
  let { data } = $props();

  const { t } = getI18n();

  // Decorative bot avatar; premium swaps the mark. The status text beside it
  // already names the state, so its alt stays empty (set in BotStatusPanel).
  const logo = $derived(data.isPremium ? '/premium-logo.png' : '/logo.png');
  // A delegate browsing the owner's board sees the connection read-only: every
  // enable/restart/disconnect action 403s for a delegate session server-side.
  const isDelegate = $derived(!!data.delegateOf);

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

    // Open only for a CONFIRMED-empty account (read succeeded, zero commands).
    // A failed read reports total 0 too; onboarding an existing user mid-outage
    // is the bug this guards against.
    data.commands.then((cd) => {
      if (cd.ok && cd.total === 0) onboardOpen = true;
    });
  });

  function finishOnboarding() {
    onboardOpen = false;
    onboardForm?.requestSubmit();
  }

  // The awaited home connection: honest per-read signals + derived UI state.
  type Conn = { signals: ConnSignals; ui: ConnUi };

  // Fold the live /substate poll (`sub`, when set) over the SSR signals and
  // re-derive the same honest UI state the server computed. One mapping, one
  // source of truth — the poll can't invent a state the server can't.
  function liveUi(c: Conn): ConnUi {
    return sub ? connectionUiState({ ...c.signals, sub: sub.state as ConnSignals['sub'] }) : c.ui;
  }

  const statusLabel = (s: string) =>
    s === 'unknown'
      ? t('overview.planUnknown')
      : t(`planLabel.${(['free', 'paid', 'vip'].includes(s) ? s : 'free')}`);

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

  // Inline error surfaced inside the confirm modal when an action fails, so the
  // modal stays open instead of closing on a rejected request.
  let actionError = $state('');

  function openModal(action: PendingAction) {
    actionError = '';
    pending = action;
  }

  function closeModal() {
    actionError = '';
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

  // Poll while the enroll is unsettled, backstopped at ~30s so a stuck job
  // stops spinning. 'unenrolled' counts as unsettled here: a just-published
  // enroll job reads as 'unenrolled' until outgress picks it up and writes
  // 'pending', and stopping in that gap would freeze the pill mid-flight.
  function startPolling() {
    stopPolling();
    let ticks = 0;
    pollTimer = setInterval(async () => {
      ticks += 1;
      const state = await refreshSub();
      if ((state !== 'pending' && state !== 'unenrolled') || ticks >= 12) stopPolling();
    }, 2500);
  }

  // Mark reconnecting immediately on user action, then poll to the outcome.
  function trackReconnect() {
    sub = { state: 'pending', error: '' };
    startPolling();
  }

  onMount(() => {
    refreshSub().then(async (state) => {
      if (state === 'pending') return startPolling();
      // The load-time self-heal reports 'pending' before outgress has written
      // anything, so a fresh poll can still read 'unenrolled'. Poll through
      // that gap only when the server said an enroll is actually in flight.
      if (state === 'unenrolled' && (await data.conn).signals.sub === 'pending') startPolling();
    });
    return stopPolling;
  });

  // A failed action must never look successful. Inspect the ActionResult:
  // only `success` closes the modal and starts reconnect tracking; a failure
  // keeps the modal open with an inline error (the RPC did not land).
  type Enhanced = {
    result: ActionResult;
    update: (opts?: { reset?: boolean; invalidateAll?: boolean }) => Promise<void>;
  };

  function closeAfterSubmit() {
    const wasRestart = pending === 'restart';
    return async ({ result, update }: Enhanced) => {
      if (result.type === 'success') {
        await update();
        closeModal();
        // Only a restart re-enrolls; disconnect just tears down.
        if (wasRestart) trackReconnect();
      } else {
        await update({ reset: false });
        actionError = t('overview.actionFailed');
      }
    };
  }

  function enableSubmit() {
    return async ({ result, update }: Enhanced) => {
      if (result.type === 'success') {
        await update();
        trackReconnect();
      } else {
        await update({ reset: false });
        toast('err', t('overview.actionFailed'));
      }
    };
  }
</script>

<section class="screen active">
  <!-- 1. Compact head: the one <h1> (focus target) with greeting + channel. -->
  <OverviewHead
    eyebrow={t('overview.eyebrow')}
    greeting={greeting}
    channel={data.displayName ?? data.login}
    description={t('overview.description')}
  />

  <!-- 2. Bot status: the page anchor. Textual state (main's honest ConnUi) plus
       the one recovery action that state needs. -->
  {#await data.conn}
    <BotStatusPanel loading logoSrc={logo} checkingText={t('overview.checking')} />
  {:then c}
    {@const u = liveUi(c)}
    <BotStatusPanel
      ui={u}
      checkingText={t('overview.checking')}
      {isDelegate}
      isPremium={data.isPremium}
      logoSrc={logo}
      planLabel={c.signals.status === 'unknown' ? undefined : statusLabel(c.signals.status)}
      onRestart={() => openModal('restart')}
      onDisconnect={() => openModal('disconnect')}
      enableSubmit={enableSubmit}
    />
  {/await}

  <!-- 3. Needs attention: only real, non-connection issues (guarded on the read
       having landed); the connection story stays in the status panel. -->
  {#await Promise.all([data.commands, data.shares]) then [cd, sh]}
    <NeedsAttention
      active={cd.active}
      total={cd.total}
      commandsOk={cd.ok}
      pendingShares={sh.pending}
      sharesOk={sh.ok}
    />
  {/await}

  <!-- 4. Quick actions: New command is the page's single primary CTA. -->
  {#await data.conn}
    <QuickActions />
  {:then c}
    <QuickActions needsAttention={liveUi(c).kind !== 'online'} />
  {/await}

  <!-- 5. Linked summary: each item a real link naming its count + destination. -->
  {#await Promise.all([data.commands, data.modules, data.conn, data.shares])}
    <section class="ov-loading" aria-busy="true" aria-label={t('overview.checking')}>
      <span class="sr-only">{t('overview.checking')}</span>
      <div class="ov-loading__grid" aria-hidden="true">
        {#each [0, 1, 2, 3] as i (i)}<Skeleton variant="block" height="56px" />{/each}
      </div>
    </section>
  {:then [cd, md, c, sh]}
    <LinkedSummary
      active={cd.active}
      commandsOk={cd.ok}
      modulesOn={md.on}
      modulesOk={md.ok}
      planLabel={statusLabel(c.signals.status)}
      people={sh.people}
      sharesOk={sh.ok}
    />
  {/await}

  <!-- 6. Established -> top commands; unreachable -> honest notice; incomplete ->
       setup guidance. -->
  {#await Promise.all([data.commands, data.conn, data.modules])}
    <section class="ov-loading" aria-busy="true" aria-label={t('overview.checking')}>
      <span class="sr-only">{t('overview.checking')}</span>
      <div class="ov-loading__stack" aria-hidden="true">
        {#each [0, 1, 2] as i (i)}<Skeleton variant="block" height="52px" />{/each}
      </div>
    </section>
  {:then [cd, c, md]}
    {#if !cd.ok}
      <!-- A failed read is not an empty account: surface the outage with a retry
           rather than a misleading "create your first command". -->
      <section class="ov-top" aria-labelledby="ov-cmd-h">
        <h2 id="ov-cmd-h" class="ov-section-h">{t('overview.topCommands')}</h2>
        <Card>
          <div class="ov-unavail">
            <p class="ov-unavail__text">
              <b>{t('overview.commandsUnavailable')}</b>
              <span>{t('overview.commandsUnavailableDesc')}</span>
            </p>
            <ButtonLink href="/" variant="ghost" class="ov-cta">{t('overview.retry')}</ButtonLink>
          </div>
        </Card>
      </section>
    {:else if cd.top.length}
      <TopCommands top={cd.top} />
    {:else}
      <SetupProgress receiving={liveUi(c).live} hasCommands={cd.total > 0} modulesOn={md.on > 0} />
    {/if}
  {/await}
</section>

<!-- First-visit setup stepper -->
<OnboardingModal open={onboardOpen} onDone={finishOnboarding} />

<form method="POST" action="?/onboarded" use:enhance bind:this={onboardForm} hidden></form>

<!-- Confirm modal -->
<Modal open={pending !== null} title={modalTitle} closeModal={closeModal}>
  {#if pending !== null}
    <p class="modal-body">{modalBody}</p>
    {#if actionError}<p class="modal-error" role="alert">{actionError}</p>{/if}
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
  .ov-loading {
    margin-bottom: var(--row-gap);
  }
  .ov-loading__grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 10px;
  }
  .ov-loading__stack {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  @media (max-width: 560px) {
    .ov-loading__grid {
      grid-template-columns: 1fr;
    }
  }

  /* Commands-unavailable notice shares the section heading rhythm with the
     TopCommands / SetupProgress components it stands in for. */
  .ov-top {
    margin-bottom: var(--row-gap);
  }
  .ov-section-h {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 16px;
    letter-spacing: -0.01em;
    color: var(--bb-white);
    margin: 0 0 12px;
  }
  .ov-unavail {
    display: flex;
    align-items: center;
    gap: 16px;
    flex-wrap: wrap;
  }
  .ov-unavail__text {
    flex: 1;
    min-width: 0;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .ov-unavail__text b {
    font-family: var(--bb-font-body);
    font-size: 14px;
    color: var(--bb-white);
  }
  .ov-unavail__text span {
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-muted);
  }
  .ov-unavail :global(.ov-cta) {
    flex: none;
    min-height: 44px;
  }
</style>
