<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit/i18n/context';
  import { giveawayDrawState } from '@bagel/kit/giveaway';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import StatTile from '@bagel/ui/svelte/StatTile.svelte';
  import { giveawayExclusionLabel } from '#lib/giveaway-workflow';
  import type { GiveawayPreviewWire } from '#lib/server/giveaways';

  let { preview }: { preview?: GiveawayPreviewWire } = $props();
  const { locale, t } = getI18n();
  const number = new Intl.NumberFormat(locale);
  const capacity = $derived(giveawayDrawState(preview?.capabilities));
  const warning = $derived(Boolean(preview?.durationProtectionWarnings?.length) || capacity.blocked || capacity.pending);
  const exclusions = $derived(Object.entries(preview?.exclusions ?? {}).filter(([, count]) => count > 0));

  const categories = ['free', 'premium', 'subscribers', 'excluded'] as const;
</script>

<Card as="article" style="min-inline-size: 0">
  <Stack gap={3}>
    <CardHead title={t('admin.giveaways.poolTitle')} headingLevel={2} />
    <div class="pool" aria-live="polite">
      {#if preview}
          <Stack gap={3}>
            <StatTile label={t('admin.giveaways.eligible')} value={number.format(preview.eligible.eligible)} fitCopy static />
            <FactList>
              {#each categories as category (category)}
                <Fact term={t(`admin.giveaways.${category}`)} fit>{number.format(preview.eligible[category])}</Fact>
              {/each}
            </FactList>
            {#if exclusions.length}
              <CardHead title={t('admin.giveaways.exclusionReasons')} headingLevel={3} />
              <FactList>
                {#each exclusions as [reason, count] (reason)}
                  <Fact term={giveawayExclusionLabel(reason, t)} fit>{number.format(count)}</Fact>
                {/each}
              </FactList>
            {/if}
            {#if warning}
              <AlertBanner tone="warning" role="note" flush>{t('admin.giveaways.protectionWarning')}</AlertBanner>
            {/if}
          </Stack>
      {:else}
        <EmptyState title={t('admin.giveaways.previewEmpty')} body={t('admin.giveaways.notLive')} />
      {/if}
    </div>
  </Stack>
</Card>

<style>
  .pool {
    min-block-size: 24rem;
  }
</style>
