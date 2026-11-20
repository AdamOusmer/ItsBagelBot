<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Table from '@bagel/ui/svelte/Table.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import { actionPayload, formatPointValue, getI18n, type LoyaltyStanding } from '@bagel/kit';
  import type { FreezeStamp } from '#lib/server/loyalty-store';

  let { top, topFreeze, pointsName, onEarning }: {
    top: LoyaltyStanding[];
    topFreeze: FreezeStamp | null;
    pointsName: string;
    onEarning: () => void;
  } = $props();
  const { t, locale } = getI18n();
  const pointsLabel = $derived(pointsName.trim() || t('loyalty.fieldNamePh'));
  let timeZone = $state('UTC');
  $effect(() => { timeZone = new Intl.DateTimeFormat().resolvedOptions().timeZone; });

  const timeFmt = $derived(new Intl.DateTimeFormat(locale, { timeZone, hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }));
  const topCaption = $derived(topFreeze
    ? t('loyalty.topPausedAt', { time: timeFmt.format(topFreeze.asOf) })
    : t('loyalty.topCaption'));
  function hours(seconds: number): string {
    const n = new Intl.NumberFormat(locale, { minimumFractionDigits: 1, maximumFractionDigits: 1 });
    return t('loyalty.hoursShort', { n: n.format(seconds / 3600) });
  }

  let resetOpen = $state(false);
  let resetBusy = $state(false);
  let resetFormEl = $state<HTMLFormElement | null>(null);

  function refusalReason(result: unknown) {
    return actionPayload<{ reason?: string }>(result)?.reason;
  }

  const resetSubmit: SubmitFunction = () => {
    resetBusy = true;
    return async ({ result }) => {
      resetBusy = false;
      resetOpen = false;
      if (result.type !== 'success') {
        const frozen = refusalReason(result) === 'frozen';
        toast('danger', t(frozen ? 'loyalty.toastResetFrozen' : 'loyalty.toastResetFailed'));
        if (frozen) await invalidateAll();
        return;
      }
      toast('success', t('loyalty.toastReset'));
      await invalidateAll();
    };
  };

</script>

  <section class="block" aria-labelledby="loy-top-h">
    <Heading level={6} as="h2" variant="title" id="loy-top-h">{t('loyalty.topTitle')}</Heading>
    <Card>
      {#if top.length === 0}
        <EmptyState title={t('loyalty.topEmpty')}>
          <Button variant="ghost" onclick={onEarning}>{t('loyalty.topEmptyCta')}</Button>
        </EmptyState>
      {:else}
        <div class="freeze-copy" class:freeze-inactive={!topFreeze} aria-hidden={!topFreeze}>
          <Text as="span" size="xs" tone="muted"><bb-fit mode="block">{topCaption}</bb-fit></Text>
        </div>
        <Table label={topCaption}>
          <caption class="bb-sr-only">{topCaption}</caption>
          <thead>
            <tr>
              <th scope="col" class="r">{t('loyalty.colRank')}</th>
              <th scope="col">{t('loyalty.colViewer')}</th>
              <th scope="col" class="r">{t('loyalty.colPoints')}</th>
              <th scope="col" class="r">{t('loyalty.colWatch')}</th>
            </tr>
          </thead>
          <tbody>
            {#each top as row, i (row.viewerId)}
              <tr>
                <th scope="row" class="r rank"><Text as="span" size="sm" tone="muted" mono>{i + 1}</Text></th>
                <td>{row.viewerName || row.viewerLogin || row.viewerId}</td>
                <td class="r">{formatPointValue(row.points, locale)}</td>
                <td class="r mut">{hours(row.watchSeconds)}</td>
              </tr>
            {/each}
          </tbody>
        </Table>
      {/if}
    </Card>
  </section>

  <section class="block" aria-labelledby="loy-reset-h">
    <Heading level={6} as="h2" variant="title" id="loy-reset-h">{t('loyalty.resetTitle')}</Heading>
    <Card>
      <div class="hint"><Text size="xs" tone="muted">{t('loyalty.resetHint', { name: pointsLabel })}</Text></div>
      <div class="actions">
        <Button variant="secondary" tone="danger" type="button" onclick={() => (resetOpen = true)}>
          {t('loyalty.resetButton')}
        </Button>
      </div>
      <form method="POST" action="?/reset" use:enhance={resetSubmit} bind:this={resetFormEl} hidden>
        <input type="hidden" name="confirm" value="reset" />
      </form>
    </Card>
  </section>

<ConfirmDialog
  bind:open={resetOpen}
  tone="danger"
  title={t('loyalty.resetConfirmTitle')}
  body={t('loyalty.resetConfirmBody', { name: pointsLabel })}
  confirmLabel={t('loyalty.resetConfirm')}
  busy={resetBusy}
  onConfirm={() => resetFormEl?.requestSubmit()}
/>

<style>
  .block { display: grid; gap: 12px; margin-bottom: 26px; }
  .hint { margin-bottom: 14px; }
  .actions { display: flex; align-items: center; justify-content: flex-end; gap: 12px; margin-top: 4px; }
  .freeze-copy { display: grid; block-size: 2.5em; }
  .freeze-inactive { opacity: 0; }
  .rank { font-variant-numeric: tabular-nums; }
</style>
