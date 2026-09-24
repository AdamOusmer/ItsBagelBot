<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import { onMount } from 'svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Modal from '@bagel/ui/svelte/Modal.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import OverviewGrid from '@bagel/ui/svelte/OverviewGrid.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { connectionUiState, type ConnSignals, type ConnUi } from '@bagel/kit/connection-state';
  import { toast } from '@bagel/ui/svelte/toast';
  import type { ActionResult } from '@sveltejs/kit';
  import BotStatusPanel from '$lib/components/overview/BotStatusPanel.svelte';
  import NeedsAttention from '$lib/components/overview/NeedsAttention.svelte';
  import QuickActions from '$lib/components/overview/QuickActions.svelte';
  import LinkedSummary from '$lib/components/overview/LinkedSummary.svelte';
  import TopCommands from '$lib/components/overview/TopCommands.svelte';
  import SetupProgress from '$lib/components/overview/SetupProgress.svelte';
  import StreamSection from '$lib/components/overview/StreamSection.svelte';
  import ActivityLog from '$lib/components/overview/ActivityLog.svelte';
  import AnsweredTonight from '$lib/components/overview/AnsweredTonight.svelte';
  import { livePoll } from '@bagel/kit/live-poll';
  import {
    CONNECTION_POLL_FAST_MS,
    CONNECTION_POLL_TIMEOUT_MS,
    connectionPollDelay,
    connectionPollSettled,
    type ConnectionPollGoal
  } from '$lib/connection-poll';
  import type {
    StreamMeta,
    StreamCounters,
    ChatVolume,
    ActivityFeed,
    AnsweredTonight as AnsweredDigest
  } from '$lib/overview-live';
  let { data } = $props();

  const { t } = getI18n();

  const logo = $derived(data.isPremium ? '/premium-logo.png' : '/logo.png');
  const isDelegate = $derived(!!data.delegateOf);

  type Conn = { signals: ConnSignals; ui: ConnUi };

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

  let now = $state(Date.now());
  onMount(() => {
    const id = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(id);
  });

  let live = $state<{
    stream: StreamMeta;
    counters: StreamCounters;
    volume: ChatVolume;
    feed: ActivityFeed;
    answered: AnsweredDigest;
  } | null>(null);
  onMount(() => {
    if (typeof EventSource === 'undefined' || isDelegate) return;
    const es = new EventSource('/overview/stream');
    es.addEventListener('live', (e) => {
      try {
        live = JSON.parse((e as MessageEvent).data);
      } catch {}
    });
    return () => es.close();
  });

  type PendingAction = 'restart' | 'disconnect' | null;
  let pending = $state<PendingAction>(null);

  const modalTitle = $derived(
    pending === 'restart' ? t('overview.modalRestartTitle') : t('overview.modalDisconnectTitle')
  );
  const modalBody = $derived(
    pending === 'restart' ? t('overview.modalRestartBody') : t('overview.modalDisconnectBody')
  );
  const modalAction = $derived(pending === 'restart' ? '?/restart' : '?/disconnect');

  let actionError = $state('');

  function openModal(action: PendingAction) {
    if (actionBusy) return;
    actionError = '';
    pending = action;
  }

  function closeModal(force = false) {
    if (actionBusy && !force) return;
    actionError = '';
    pending = null;
  }

  let sub = $state<{ state: string; error: string } | null>(null);
  let actionBusy = $state(false);
  let stopPoll: (() => void) | null = null;

  function stopPolling(clearBusy = true) {
    const stop = stopPoll;
    stopPoll = null;
    stop?.();
    if (clearBusy) actionBusy = false;
  }

  async function refreshSub(): Promise<string> {
    try {
      const r = await fetch('/substate');
      if (r.ok) {
        sub = await r.json();
        return sub?.state ?? 'unknown';
      }
    } catch {}
    return 'unknown';
  }

  function startPolling(goal: ConnectionPollGoal) {
    stopPolling(false);
    actionBusy = true;
    const started = Date.now();
    let sawUnsettled = false;

    stopPoll = livePoll(
      async () => {
        const state = await refreshSub();
        if (state === 'pending' || state === 'unenrolled') sawUnsettled = true;
        return connectionPollSettled(goal, state, Date.now() - started, sawUnsettled);
      },
      {
        firstDelayMs: CONNECTION_POLL_FAST_MS,
        delayMs: connectionPollDelay,
        timeoutMs: CONNECTION_POLL_TIMEOUT_MS,
        onDone: () => {
          stopPoll = null;
          actionBusy = false;
        }
      }
    );
  }

  function trackReconnect() {
    sub = { state: 'pending', error: '' };
    startPolling('connected');
  }

  onMount(() => {
    refreshSub().then(async (state) => {
      if (state === 'pending') return startPolling('connected');
      if (state === 'unenrolled' && (await data.conn).signals.sub === 'pending') startPolling('connected');
    });
    return stopPolling;
  });

  type Enhanced = {
    result: ActionResult;
    update: (opts?: { reset?: boolean; invalidateAll?: boolean }) => Promise<void>;
  };

  function closeAfterSubmit() {
    const action = pending;
    actionBusy = true;
    return async ({ result, update }: Enhanced) => {
      if (result.type === 'success') {
        await update();
        closeModal(true);
        if (action === 'restart') trackReconnect();
        else startPolling('disconnected');
      } else {
        await update({ reset: false });
        actionBusy = false;
        actionError = t('overview.actionFailed');
      }
    };
  }

  function enableSubmit() {
    actionBusy = true;
    return async ({ result, update }: Enhanced) => {
      if (result.type === 'success') {
        await update();
        trackReconnect();
      } else {
        await update({ reset: false });
        actionBusy = false;
        toast('err', t('overview.actionFailed'));
      }
    };
  }
</script>

<section class="screen active">
  <PageHead
    compact
    eyebrow={t('overview.eyebrow')}
    description={t('overview.description')}
  >{greeting}, <em>{data.displayName ?? data.login}</em></PageHead>

  {#await data.conn}
    <BotStatusPanel loading logoSrc={logo} checkingText={t('overview.checking')} />
  {:then c}
    {@const u = liveUi(c)}
    <BotStatusPanel
      ui={u}
      checkingText={t('overview.checking')}
      busy={actionBusy}
      {isDelegate}
      isPremium={data.isPremium}
      logoSrc={logo}
      planLabel={c.signals.status === 'unknown' ? undefined : statusLabel(c.signals.status)}
      onRestart={() => openModal('restart')}
      onDisconnect={() => openModal('disconnect')}
      enableSubmit={enableSubmit}
    />
  {/await}

  {#await Promise.all([data.stream, data.counters, data.volume])}
    <section class="ov-loading" aria-busy="true" aria-label={t('overview.checking')}>
      <span class="bb-sr-only">{t('overview.checking')}</span>
      <SkeletonStack rows={1} height="260px" />
    </section>
  {:then [meta, counters, volume]}
    <StreamSection
      meta={live?.stream ?? meta}
      counters={live?.counters ?? counters}
      volume={live?.volume ?? volume}
      {now}
    />
  {/await}

  <OverviewGrid>
    {#snippet main()}
      {#await data.feed}
        <Skeleton variant="block" height="420px" />
      {:then feed}
        <ActivityLog feed={live?.feed ?? feed} />
      {/await}
    {/snippet}

    {#snippet side()}
      {#await data.answered}
        <Skeleton variant="block" height="260px" />
      {:then answered}
        <AnsweredTonight answered={live?.answered ?? answered} />
      {/await}

      {#await Promise.all([data.commands, data.shares]) then [cd, sh]}
        <NeedsAttention
          active={cd.active}
          total={cd.total}
          commandsOk={cd.ok}
          pendingShares={sh.pending}
          sharesOk={sh.ok}
        />
      {/await}

      {#await Promise.all([data.commands, data.modules, data.conn, data.shares])}
        <SkeletonStack rows={4} height="56px" columns={2} />
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
    {/snippet}
  </OverviewGrid>

  {#await data.conn}
    <QuickActions />
  {:then c}
    <QuickActions needsAttention={liveUi(c).kind !== 'online'} />
  {/await}

  {#await Promise.all([data.commands, data.conn, data.modules])}
    <section class="ov-loading" aria-busy="true" aria-label={t('overview.checking')}>
      <span class="bb-sr-only">{t('overview.checking')}</span>
      <SkeletonStack rows={3} height="52px" />
    </section>
  {:then [cd, c, md]}
    {#if !cd.ok}
      <section class="ov-top" aria-labelledby="ov-cmd-h">
        <h2 id="ov-cmd-h" class="ov-section-h">{t('overview.topCommands')}</h2>
        <Card>
          <div class="ov-unavail">
            <p class="ov-unavail__text">
              <b>{t('overview.commandsUnavailable')}</b>
              <Text as="span" size="sm" tone="muted">{t('overview.commandsUnavailableDesc')}</Text>
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

<Modal open={pending !== null} title={modalTitle} closeModal={closeModal}>
  {#if pending !== null}
    <p class="modal-body">{modalBody}</p>
    {#if actionError}<p class="modal-error" role="alert">{actionError}</p>{/if}
    <form method="POST" action={modalAction} use:enhance={closeAfterSubmit} class="modal-actions">
      <Button variant="ghost" type="button" disabled={actionBusy} onclick={() => closeModal()}>{t('common.cancel')}</Button>
      <Button
        variant={pending === 'disconnect' ? 'tan' : 'primary'}
        type="submit"
        loading={actionBusy}
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
  .ov-unavail :global(.ov-cta) {
    flex: none;
    min-height: 44px;
  }
</style>
