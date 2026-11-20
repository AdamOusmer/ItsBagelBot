<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n, MOD, moduleDef } from '@bagel/kit';
  import LoyaltyGameRow from './LoyaltyGameRow.svelte';

  let { games, enabled }: {
    games: { id: string; enabled: boolean; config: Record<string, string>; revision: number }[];
    enabled: boolean;
  } = $props();

  const { t } = getI18n();
  const raffleHref = moduleDef(MOD.raffle)!.href!;
</script>

<section class="block" aria-labelledby="loy-games-h">
  <Heading level={6} as="h2" variant="title" id="loy-games-h">{t('loyalty.gamesTitle')}</Heading>
  <Card>
    <div class="hint"><Text size="xs" tone="muted">{t(enabled ? 'loyalty.gamesHint' : 'loyalty.gamesNeedsOn')}</Text></div>
    <div class="games">
      {#each games as settings (settings.id)}
        {@const def = moduleDef(settings.id)}
        {#if def}
          <LoyaltyGameRow {def} {settings} loyaltyOn={enabled} />
        {/if}
      {/each}
    </div>
  </Card>
  <Card>
    <ButtonLink href={raffleHref} variant="secondary"><bb-fit><span data-fit="">{t('nav.giveaways')}</span></bb-fit></ButtonLink>
  </Card>
</section>

<style>
  .games { display: grid; gap: 8px; }
  .block { display: grid; gap: 12px; margin-bottom: 26px; }
  .hint { margin-bottom: 14px; }
</style>
