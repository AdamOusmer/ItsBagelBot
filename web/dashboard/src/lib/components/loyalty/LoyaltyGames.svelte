<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n, moduleDef } from '@bagel/kit';
  import PageNotice from '#lib/components/shared/PageNotice.svelte';
  import LoyaltyGameRow from './LoyaltyGameRow.svelte';

  let { games, enabled, unpaid = [] }: {
    games: { id: string; enabled: boolean; config: Record<string, string>; revision: number }[];
    enabled: boolean;
    unpaid?: string[];
  } = $props();

  const { t } = getI18n();
</script>

<section class="block" aria-labelledby="loy-games-h">
  <Heading level={6} as="h2" variant="title" id="loy-games-h">{t('loyalty.gamesTitle')}</Heading>
  <PageNotice visible={unpaid.length > 0} tone="warning" role="status">
    {t('loyalty.raffleUnpaid', { viewers: unpaid.map((name) => '@' + name).join(', ') })}
  </PageNotice>
  <Card>
    <div class="hint" class:inactive={enabled} inert={enabled}>
      <Text size="xs" tone="muted">{t('loyalty.gamesNeedsOn')}</Text>
    </div>
    <div class="games">
      {#each games as settings (settings.id)}
        {@const def = moduleDef(settings.id)}
        {#if def}
          <LoyaltyGameRow {def} {settings} loyaltyOn={enabled} />
        {/if}
      {/each}
    </div>
  </Card>
</section>

<style>
  .games { display: grid; gap: 8px; }
  .block { display: grid; gap: 12px; margin-bottom: 26px; }
  .hint { margin-bottom: 14px; transition: opacity var(--bb-dur-fast); }
  .inactive { opacity: 0; }
  @media (prefers-reduced-motion: reduce) {
    .hint { transition: none; }
  }
</style>
