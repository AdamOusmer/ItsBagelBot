<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '@bagel/ui/styles/elements/collapse.css';
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
  <PageNotice visible={unpaid.length > 0} tone="warning" role="status" gap="var(--bb-space-3)">
    {t('loyalty.raffleUnpaid', { viewers: unpaid.map((name) => '@' + name).join(', ') })}
  </PageNotice>
  <Card>
    <div class="bb-collapse" class:is-open={!enabled} inert={enabled} aria-hidden={enabled}>
      <div><div class="hint"><Text size="xs" tone="muted">{t('loyalty.gamesNeedsOn')}</Text></div></div>
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
  .block { display: flex; flex-direction: column; gap: var(--bb-space-3); margin-bottom: var(--bb-space-5); }
  .hint { padding-block-end: var(--bb-space-4); }
</style>
