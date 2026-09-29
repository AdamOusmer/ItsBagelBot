<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';

  const { t } = getI18n();

  let { needsAttention = false }: { needsAttention?: boolean } = $props();
</script>

<section class="ov-quick" aria-labelledby="ov-quick-h">
  <Heading level={6} as="h2" variant="title" id="ov-quick-h">{t('overview.quickHeading')}</Heading>
  <div class="ov-quick__row">
    <ButtonLink href="/commands" variant="primary">{t('overview.quickNewCommand')}</ButtonLink>
    <ButtonLink href="/modules" variant="ghost">{t('overview.quickModules')}</ButtonLink>
    <ButtonLink href="/songqueue" variant="ghost">{t('nav.songqueue')}</ButtonLink>
    {#if needsAttention}
      <ButtonLink href="/settings" variant="ghost">{t('overview.quickSettings')}</ButtonLink>
    {/if}
  </div>
</section>

<style>
  .ov-quick {
    display: grid;
    gap: 12px;
    margin-bottom: var(--row-gap);
  }
  .ov-quick__row {
    --btn-min-h: 44px;
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
  }
  @media (max-width: 480px) {
    .ov-quick__row {
      --btn-w: 100%;
    }
  }
</style>
