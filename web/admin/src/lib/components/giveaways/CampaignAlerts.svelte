<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit/i18n/context';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import SkeletonStack from '@bagel/ui/svelte/SkeletonStack.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { giveawayDate } from '#lib/giveaway-workflow';
  import type { GiveawayAlertWire } from '#lib/server/giveaways';

  let { alerts }: { alerts: Promise<GiveawayAlertWire[] | null> } = $props();
  const { locale, t } = getI18n();
  const formatter = new Intl.DateTimeFormat(locale, {
    year: 'numeric', month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit', timeZone: 'UTC', timeZoneName: 'short'
  });
</script>

<Card as="section">
  <Stack gap={3}>
    <CardHead title={t('admin.giveaways.alerts')} headingLevel={2} />
    <div class="alert-window">
      {#await alerts}
        <div role="status" aria-busy="true" aria-label={t('common.loading')}><SkeletonStack rows={2} height="5rem" /></div>
      {:then rows}
        {#if !rows}
          <AlertBanner tone="danger" flush>{t('admin.giveaways.alertsUnavailable')}</AlertBanner>
        {:else if rows.length === 0}
          <EmptyState title={t('admin.giveaways.noAlerts')} />
        {:else}
            <Stack as="ul" gap={3} class="giveaway-alert-list">
              {#each rows as alert (alert.id)}
                <li class="alert-row">
                  <AlertBanner tone={alert.urgent ? 'danger' : 'warning'} role="note" flush>{alert.reason}</AlertBanner>
                  <FactList>
                    <Fact term={t('admin.giveaways.award')} fit><Text as="span" size="xs" mono>{alert.awardId}</Text></Fact>
                    <Fact term={t('admin.giveaways.nextCharge')} fit>{giveawayDate(alert.upcomingChargeAt, formatter, t)}</Fact>
                    <Fact term={t('admin.giveaways.pendingSince')} fit>{giveawayDate(alert.pendingSince, formatter, t)}</Fact>
                  </FactList>
                </li>
              {/each}
            </Stack>
        {/if}
      {/await}
    </div>
  </Stack>
</Card>

<style>
  .alert-window {
    min-block-size: 11rem;
  }

  :global(.giveaway-alert-list) {
    padding: 0;
    margin: 0;
    list-style: none;
  }

  .alert-row {
    display: grid;
    gap: var(--bb-space-2);
    min-inline-size: 0;
  }
</style>
