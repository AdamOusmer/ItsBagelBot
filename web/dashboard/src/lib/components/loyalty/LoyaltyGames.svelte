<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { catalogChildren, getI18n } from '@bagel/kit';
  import LoyaltyGameRow from './LoyaltyGameRow.svelte';

  let { games, unpaid = [], enabled }: {
    games: { id: string; enabled: boolean; config: Record<string, string>; revision: number }[];
    unpaid?: string[];
    enabled: boolean;
  } = $props();

  const { t } = getI18n();
  const defs = catalogChildren('loyalty');
</script>

<section class="block" aria-labelledby="loy-games-h">
  <Heading level={6} as="h2" variant="title" id="loy-games-h">{t('loyalty.gamesTitle')}</Heading>
  {#if unpaid.length}
    <AlertBanner tone="warning">{t('loyalty.raffleUnpaid', { viewers: unpaid.map((name) => '@' + name).join(', ') })}</AlertBanner>
  {/if}
  <Card>
    <div class="hint"><Text size="xs" tone="muted">{t(enabled ? 'loyalty.gamesHint' : 'loyalty.gamesNeedsOn')}</Text></div>
    <div class="games">
      {#each defs as def (def.id)}
        {@const settings = games.find((game) => game.id === def.id)}
        {#if settings}
          <LoyaltyGameRow {def} {settings} loyaltyOn={enabled} />
        {/if}
      {/each}
    </div>
  </Card>
</section>

<style>
  .games { display: grid; gap: 8px; }
  .block { display: grid; gap: 12px; margin-bottom: 26px; }
  .hint { margin-bottom: 14px; }
</style>
