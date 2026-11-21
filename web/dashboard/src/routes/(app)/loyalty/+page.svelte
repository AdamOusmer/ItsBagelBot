<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { afterNavigate } from '$app/navigation';
  import { page } from '$app/state';
  import { untrack } from 'svelte';
  import { livePoll } from '@bagel/kit/live-poll';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import DeckList from '@bagel/ui/svelte/DeckList.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import ModuleCommandList from '#lib/components/modules/ModuleCommandList.svelte';
  import LoyaltyStatus from '#lib/components/loyalty/LoyaltyStatus.svelte';
  import LoyaltyEarning from '#lib/components/loyalty/LoyaltyEarning.svelte';
  import LoyaltyStandings from '#lib/components/loyalty/LoyaltyStandings.svelte';
  import LoyaltyGames from '#lib/components/loyalty/LoyaltyGames.svelte';
  import PageNotice from '#lib/components/shared/PageNotice.svelte';
  import { createTriggerToggle, reseedOnChange } from '#lib/client-actions.svelte';
  import { getI18n, MasterToggle, moduleDef } from '@bagel/kit';
  import type { RaffleStatus } from '#lib/server/loyalty-store';

  let { data } = $props();
  const { t } = getI18n();
  const loyaltyCommands = moduleDef('loyalty')?.commands ?? [];
  const triggers = createTriggerToggle(
    () => data.disabledTriggers ?? '',
    () => toast('danger', t('modules.saveFailed'))
  );
  let enabled = $state(untrack(() => data.enabled));
  reseedOnChange(() => data.enabled, (next) => { enabled = next; });
  let raffle = $state<RaffleStatus | null>(untrack(() => data.raffle));
  reseedOnChange(() => data.raffle, (next) => { raffle = next; });

  let tab = $state(untrack(() => page.url.hash === '#games' ? 'games' : 'status'));
  const pollStatus = $derived(tab === 'status' || tab === 'games');
  const statusInterval = 5000;
  const options = $derived([
    { value: 'status', label: t('loyalty.statusTitle'), attrs: { 'aria-controls': 'loyalty-status' } },
    { value: 'earning', label: t('loyalty.earningTab'), attrs: { 'aria-controls': 'loyalty-earning' } },
    { value: 'standings', label: t('loyalty.standingsTab'), attrs: { 'aria-controls': 'loyalty-standings' } },
    { value: 'games', label: t('loyalty.gamesTitle'), attrs: { 'aria-controls': 'games' } }
  ]);

  afterNavigate(({ to }) => {
    if (to?.url.hash === '#games') tab = 'games';
  });

  $effect(() => {
    if (!pollStatus) return;
    const controller = new AbortController();
    const stop = livePoll(async () => {
      if (document.hidden) return false;
      const latest = await fetch('/loyalty/status', { signal: controller.signal })
        .then((res) => (res.ok ? res.json() as Promise<RaffleStatus | null> : null))
        .catch(() => null);
      if (!controller.signal.aborted) raffle = latest;
      return false;
    }, {
      firstDelayMs: statusInterval,
      delayMs: () => statusInterval,
      timeoutMs: Infinity,
      refreshOnVisible: true
    });
    return () => {
      controller.abort();
      stop();
    };
  });
</script>

<section class="screen active">
  <PageHead eyebrow={t('loyalty.eyebrow')} description={t('loyalty.description')}>
    {t('loyalty.titlePre')}<em>{t('loyalty.titleEm')}</em>
  </PageHead>

  <PageNotice visible={!!data.degraded}>{t('loyalty.degraded')}</PageNotice>

  <PageToolbar>
    {#snippet leading()}
      <MasterToggle
        action="?/toggle"
        bind:enabled
        label={t('loyalty.botOn')}
        hint={t('loyalty.botOnHint')}
        ariaLabel={t('loyalty.botOn')}
        failMessage={t('loyalty.toastToggleFailed')}
      />
    {/snippet}
    {#snippet trailing()}
      <ButtonLink href="/counters" variant="ghost">{t('loyalty.countersLink')}</ButtonLink>
    {/snippet}
  </PageToolbar>

  <SegmentedControl {options} bind:value={tab} label={t('loyalty.tabsLabel')} even />
  <div class="panels">
    <div id="loyalty-status" class="panel" class:inactive={tab !== 'status'} inert={tab !== 'status'}>
      <LoyaltyStatus freeze={data.freeze} health={raffle?.accrual_health} {enabled} />
      {#if loyaltyCommands.length}
        <DeckList>
          <ModuleCommandList
            moduleId="loyalty"
            commands={loyaltyCommands}
            headingId="loy-chat-h"
            disabledTriggers={triggers.value}
            status={triggers.status}
            onDisabledTriggersChange={triggers.save}
          />
        </DeckList>
      {/if}
    </div>
    <div id="loyalty-earning" class="panel" class:inactive={tab !== 'earning'} inert={tab !== 'earning'}>
      <LoyaltyEarning initial={data.config} />
    </div>
    <div id="loyalty-standings" class="panel" class:inactive={tab !== 'standings'} inert={tab !== 'standings'}>
      <LoyaltyStandings top={data.top} topFreeze={data.topFreeze} pointsName={data.config.pointsName} onEarning={() => (tab = 'earning')} />
    </div>
    <div id="games" class="panel" class:inactive={tab !== 'games'} inert={tab !== 'games'}>
      <LoyaltyGames games={data.games} unpaid={raffle?.unpaid} {enabled} />
    </div>
  </div>
</section>

<style>
  .panels { position: relative; isolation: isolate; margin-top: 16px; overflow: clip; }
  .panel { min-inline-size: 0; transition: opacity 160ms ease; }
  .inactive { position: absolute; inset: 0 0 auto; opacity: 0; pointer-events: none; }

  @media (prefers-reduced-motion: reduce) {
    .panel { transition: none; }
  }
</style>
