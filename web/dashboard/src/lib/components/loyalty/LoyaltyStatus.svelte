<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n, type MessageKey } from '@bagel/kit';
  import { FREEZE_MAX_MINUTES } from '@bagel/kit/catalog';
  import PageNotice from '#lib/components/shared/PageNotice.svelte';
  import type { FreezeView, RaffleStatus } from '#lib/server/loyalty-store';
  import { finishFreezeAction } from './freeze-action';

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

  let freezeBusy = $state(false);
  let timeZone = $state('UTC');

  $effect(() => {
    timeZone = new Intl.DateTimeFormat().resolvedOptions().timeZone;
  });

  $effect(() => {
    if (!freeze) return;
    const wait = freeze.expiresAt - Date.now();
    if (wait <= 0) return;
    const timeout = setTimeout(() => void invalidateAll(), wait);
    return () => clearTimeout(timeout);
  });

  const timeFmt = $derived(new Intl.DateTimeFormat(locale, { timeZone, hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }));
  const freezeStatus = $derived(freeze
    ? t('loyalty.freezeStatusPaused', { time: timeFmt.format(freeze.asOf), end: timeFmt.format(freeze.expiresAt) })
    : t('loyalty.freezeStatusLive'));

  const freezeSubmit: SubmitFunction = () => {
    const releasing = freeze !== null;
    freezeBusy = true;
    return async ({ result }) => {
      try {
        await finishFreezeAction(result, releasing, t);
      } finally {
        freezeBusy = false;
      }
    };
  };
</script>

<section class="block" aria-labelledby="loy-freeze-h">
  <PageNotice role="status" tone={enabled && health === 'stalled' ? 'warning' : 'info'}>
    {t(!enabled ? 'loyalty.accrualOff' : health ? healthCopy[health] : 'loyalty.accrualUnavailable')}
  </PageNotice>
  <Heading level={6} as="h2" variant="title" id="loy-freeze-h">{t('loyalty.freezeTitle')}</Heading>
  <Card>
    <div class="freeze-copy" role="status">
      <Text as="span" size="sm"><bb-fit mode="block">{freezeStatus}</bb-fit></Text>
    </div>
    <div class="freeze-copy">
      <Text as="span" size="xs" tone="muted"><bb-fit mode="block">{t('loyalty.freezeHint')}</bb-fit></Text>
    </div>
    <form method="POST" action={freeze ? '?/unfreeze' : '?/freeze'} use:enhance={freezeSubmit} class="actions">
      <div class="minutes" class:faded={!!freeze} inert={!!freeze}>
        <Field fitCopy label={t('loyalty.freezeMinutes')}>
          <Input type="number" name="minutes" min={1} max={FREEZE_MAX_MINUTES} value={FREEZE_MAX_MINUTES}
            required disabled={freezeBusy || !!freeze} fill />
        </Field>
      </div>
      <div class="freeze-button">
        <Button block type="submit" busy={freezeBusy}>
          <bb-fit><span data-fit="">{t(freeze ? 'loyalty.unfreezeButton' : 'loyalty.freezeButton')}</span></bb-fit>
        </Button>
      </div>
    </form>
  </Card>
</section>

<style>
  .block { display: grid; gap: var(--bb-space-3); margin-bottom: 26px; }
  .freeze-copy { display: grid; block-size: 2.5em; }
  .actions { display: flex; flex-wrap: wrap; align-items: end; gap: var(--bb-space-3); }
  .minutes { flex: 1 0 9rem; max-inline-size: 12rem; transition: opacity var(--bb-dur-fast); }
  .faded { opacity: 0; }
  .freeze-button { inline-size: 13rem; max-inline-size: 100%; }
  @media (prefers-reduced-motion: reduce) {
    .minutes { transition: none; }
  }
</style>
