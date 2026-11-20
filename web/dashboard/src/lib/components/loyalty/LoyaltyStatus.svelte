<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import SwitchRow from '@bagel/ui/svelte/SwitchRow.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n, type MessageKey } from '@bagel/kit';
  import { FREEZE_MAX_MINUTES } from '@bagel/kit/catalog';
  import type { FreezeView, RaffleStatus } from '#lib/server/loyalty-store';
  import { finishFreezeAction } from '../../../routes/(app)/loyalty/freeze-action';

  let { freeze, health, enabled }: {
    freeze: FreezeView | null;
    health?: RaffleStatus['accrual_health'];
    enabled: boolean;
  } = $props();
  const healthCopy: Record<RaffleStatus['accrual_health'], MessageKey> = {
    healthy: 'loyalty.accrualHealthy',
    starting: 'loyalty.accrualStarting',
    offline: 'loyalty.accrualOffline',
    stalled: 'loyalty.accrualStalled'
  };
  const { t, locale } = getI18n();

  const hasModHold = $derived(freeze?.holders.includes('mod') ?? false);
  const hasRoundHold = $derived(freeze?.holders.some((holder) => holder.startsWith('round:')) ?? false);
  let freezeBusy = $state(false);
  let freezeNow = $state(Date.now());
  let minutes = $state(FREEZE_MAX_MINUTES);
  let blockTransfer = $state(false);
  let closeGames = $state(false);
  let timeZone = $state('UTC');

  $effect(() => {
    timeZone = new Intl.DateTimeFormat().resolvedOptions().timeZone;
  });

  $effect(() => {
    if (!freeze) return;
    const now = Date.now();
    freezeNow = now;
    const wait = freeze.expiresAt - now;
    if (wait <= 0) return;
    const interval = setInterval(() => (freezeNow = Date.now()), 1000);
    const timeout = setTimeout(() => void invalidateAll(), wait);
    return () => {
      clearInterval(interval);
      clearTimeout(timeout);
    };
  });

  const timeFmt = $derived(new Intl.DateTimeFormat(locale, { timeZone, hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }));
  const freezeStatus = $derived(freeze
    ? t('loyalty.freezeStatusPaused', { time: timeFmt.format(freeze.asOf), end: timeFmt.format(freeze.expiresAt) })
    : t('loyalty.freezeStatusLive'));
  const freezeMinutesLeft = $derived(freeze ? Math.max(0, Math.ceil((freeze.expiresAt - freezeNow) / 60_000)) : null);

  const freezeSubmit: SubmitFunction = () => {
    const releasing = hasModHold;
    freezeBusy = true;
    return async ({ result }) => {
      try {
        if (result.type === 'success') {
          blockTransfer = false;
          closeGames = false;
        }
        await finishFreezeAction(result, releasing, t);
      } finally {
        freezeBusy = false;
      }
    };
  };
</script>

  <section class="block" aria-labelledby="loy-freeze-h">
  <AlertBanner role="status" tone={enabled && health === 'stalled' ? 'warning' : 'info'}>
    {t(!enabled ? 'loyalty.accrualOff' : health ? healthCopy[health] : 'loyalty.accrualUnavailable')}
  </AlertBanner>
    <Heading level={6} as="h2" variant="title" id="loy-freeze-h">{t('loyalty.freezeTitle')}</Heading>
    <Card>
      <div class="freeze-copy" role="status">
        <Text as="span" size="sm"><bb-fit mode="block">{freezeStatus}</bb-fit></Text>
      </div>
      <div class="freeze-copy">
        <Text as="span" size="xs" tone="muted">
          <bb-fit mode="block">{t(hasRoundHold ? 'loyalty.freezeRoundHold' : 'loyalty.freezeHint')}</bb-fit>
        </Text>
      </div>
      <form method="POST" action={hasModHold ? '?/unfreeze' : '?/freeze'} use:enhance={freezeSubmit}>
        <input type="hidden" name="freeze_id" value={freeze?.id ?? ''} />
        <input type="hidden" name="minutes" value={Math.max(1, freezeMinutesLeft ?? 1)} disabled={!freeze || hasModHold} />
        <fieldset class="freeze-config" class:freeze-locked={!!freeze} disabled={!!freeze || freezeBusy}>
          <Field label={t(freeze ? 'loyalty.freezeMinutesLeft' : 'loyalty.freezeMinutes')}>
            <span class="num">
              <Input
                fill
                type="number"
                name="minutes"
                min={1}
                max={FREEZE_MAX_MINUTES}
                step={1}
                required
                value={freezeMinutesLeft ?? minutes}
                oninput={(event) => (minutes = event.currentTarget.valueAsNumber)}
              />
            </span>
          </Field>
          <SwitchRow
            control="end"
            fitCopy
            label={t('loyalty.freezeBlockTransfers')}
            hint={t('loyalty.freezeBlockTransfersHint')}
            hintId="freeze-transfer-hint"
            disabled={!!freeze || freezeBusy}
            checked={freeze?.blockTransfer ?? blockTransfer}
            onCheckedChange={(checked) => (blockTransfer = checked)}
          />
          <SwitchRow
            control="end"
            fitCopy
            label={t('loyalty.freezeCloseGames')}
            hint={t('loyalty.freezeCloseGamesHint')}
            hintId="freeze-games-hint"
            disabled={!!freeze || freezeBusy}
            checked={freeze?.closeGames ?? closeGames}
            onCheckedChange={(checked) => (closeGames = checked)}
          />
          <input type="hidden" name="block_transfer" value={blockTransfer ? 'on' : ''} />
          <input type="hidden" name="close_games" value={closeGames ? 'on' : ''} />
        </fieldset>
        <div class="freeze-copy">
          <Text as="span" size="xs" tone="muted"><bb-fit mode="block">{t('loyalty.freezeLag')}</bb-fit></Text>
        </div>
        <div class="actions">
          <span class="freeze-button">
            <Button block variant="primary" type="submit" busy={freezeBusy}>
              <bb-fit>{t(hasModHold ? 'loyalty.unfreezeButton' : 'loyalty.freezeButton')}</bb-fit>
            </Button>
          </span>
        </div>
      </form>
    </Card>
  </section>

<style>
  .block { display: grid; gap: 12px; margin-bottom: 26px; }
  .freeze-copy { display: grid; block-size: 2.5em; transition: opacity 160ms ease; }
  .freeze-config {
    display: grid;
    gap: 12px;
    min-inline-size: 0;
    margin: 0 0 12px;
    border: 0;
    padding: 0;
    transition: opacity 160ms ease;
  }
  .freeze-locked { opacity: 0.7; }
  .freeze-button { inline-size: 13rem; max-inline-size: 100%; }

  @media (prefers-reduced-motion: reduce) {
    .freeze-config, .freeze-copy { transition: none; }
  }

  .num { width: 100%; max-width: 160px; }
  .actions { display: flex; align-items: center; justify-content: flex-end; gap: 12px; margin-top: 4px; }
</style>
