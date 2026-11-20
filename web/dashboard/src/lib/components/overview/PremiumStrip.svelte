<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Bolota from '@bagel/ui/svelte/Bolota.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';

  const { t } = getI18n();
  const gold = { head: '#d8bf91', eye: '#111113' };
</script>

<Card as="section" tone="gold" class="ov-premium" aria-label={t('overview.premiumTitle')}>
  <span class="ov-premium__mascot"><Bolota name="oven_fresh" size={48} palette={gold} motionState="idle" active gate plate={false} /></span>
  <span class="ov-premium__copy">
    <Text as="span" size="sm"><strong><bb-fit>{t('overview.premiumTitle')}</bb-fit></strong></Text>
    <Text as="span" size="xs" tone="muted"><bb-fit>{t('overview.premiumLine')}</bb-fit></Text>
  </span>
  <ButtonLink href="/billing?from=overview" size="sm" variant="secondary">{t('overview.premiumCta')}</ButtonLink>
</Card>

<style>
  :global(.ov-premium) {
    display: flex;
    align-items: center;
    gap: var(--bb-space-4);
    padding: var(--bb-space-3) var(--bb-space-4);
    margin-bottom: var(--row-gap);
  }
  .ov-premium__mascot {
    flex: none;
    width: 48px;
    height: 48px;
    filter: drop-shadow(0 0 12px rgba(var(--bb-tan-rgb), 0.35));
  }
  .ov-premium__copy {
    display: grid;
    gap: 2px;
    flex: 1;
    min-width: 0;
  }
  @media (max-width: 760px) {
    :global(.ov-premium) {
      flex-wrap: wrap;
    }
  }
</style>
