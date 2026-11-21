<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import StatePill from '#lib/components/StatePill.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { GiveawayAlertWire } from '#lib/server/giveaways';

  type Alert = GiveawayAlertWire & { campaignId?: string };

  let { alerts, ok }: { alerts: Alert[]; ok: boolean } = $props();

  const { t, locale } = getI18n();
  const format = new Intl.NumberFormat(locale);
</script>

<Card as="section" class={alerts.length ? 'overview-awards-panel overview-awards-pending' : 'overview-awards-panel'}>
  <CardHead title={t('admin.giveaways.alerts')} headingLevel={2}>
    {#snippet actions()}
      {#if alerts.length}
        <StatePill tone="warning">{format.format(alerts.length)}</StatePill>
      {/if}
    {/snippet}
  </CardHead>

  {#if !ok}
    <AlertBanner>{t('admin.giveaways.alertsUnavailable')}</AlertBanner>
  {:else if alerts.length}
    <Scroller fill role="region" aria-label={t('admin.giveaways.alerts')} tabindex={0}>
      <Stack as="ul" class="overview-award-list" gap={0}>
        {#each alerts as alert (alert.id)}
          <li>
            <a class="award" href={alert.campaignId ? `/giveaways/${encodeURIComponent(alert.campaignId)}` : '/giveaways'}>
              <Text as="span" size="sm" mono truncate title={alert.awardId}>{alert.awardId}</Text>
              <Text as="span" size="xs" tone="muted" truncate title={alert.reason}>{alert.reason}</Text>
            </a>
          </li>
        {/each}
      </Stack>
    </Scroller>
  {:else}
    <Text size="sm" tone="muted">{t('admin.overview.giveawaysClear')}</Text>
  {/if}
</Card>

<style>
  :global(.overview-awards-panel) {
    display: flex;
    flex-direction: column;
  }

  :global(.overview-awards-pending) {
    --card-bg: color-mix(in srgb, var(--bb-warn) 10%, var(--bb-elev-1));
  }

  :global(.overview-award-list) {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .award {
    display: grid;
    gap: var(--bb-space-1);
    padding: var(--bb-space-2);
    border-radius: var(--bb-radius-sm);
    color: inherit;
    text-decoration: none;
  }

  .award:hover,
  .award:focus-visible {
    background: var(--bb-elev-2);
  }

  .award:focus-visible {
    outline: 2px solid var(--bb-tan);
    outline-offset: -2px;
  }
</style>
