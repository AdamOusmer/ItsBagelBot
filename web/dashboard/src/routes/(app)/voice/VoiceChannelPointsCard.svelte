<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { getI18n } from '@bagel/kit/i18n/context';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';

  const { t } = getI18n();
</script>

<Card as="section" aria-labelledby="voice-rewards">
  <div class="rewards">
    <div class="copy">
      <Heading level={6} as="h2" variant="eyebrow" id="voice-rewards">
        <bb-fit><span data-fit="">{t('voice.rewards.title')}</span></bb-fit>
      </Heading>
      <Text as="p" size="sm" tone="muted">{t('voice.rewards.body')}</Text>
    </div>
    <ButtonLink variant="secondary" href="/channelpoints">{t('voice.rewards.open')}</ButtonLink>
  </div>
</Card>

<style>
  .rewards { display: flex; align-items: center; gap: var(--bb-space-4); }
  .copy { display: flex; flex-direction: column; gap: var(--bb-space-1); flex: 1; min-width: 0; }
  @media (max-width: 560px) {
    .rewards { flex-direction: column; align-items: stretch; }
  }
</style>
