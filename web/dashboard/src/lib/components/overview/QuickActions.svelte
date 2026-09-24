<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';

  const { t } = getI18n();

  let { needsAttention = false }: { needsAttention?: boolean } = $props();
</script>

<section class="ov-quick" aria-labelledby="ov-quick-h">
  <h2 id="ov-quick-h" class="ov-section-h">{t('overview.quickHeading')}</h2>
  <div class="ov-quick__row">
    <ButtonLink href="/commands" variant="primary" class="ov-quick__btn">{t('overview.quickNewCommand')}</ButtonLink>
    <ButtonLink href="/modules" variant="ghost" class="ov-quick__btn">{t('overview.quickModules')}</ButtonLink>
    {#if needsAttention}
      <ButtonLink href="/settings" variant="ghost" class="ov-quick__btn">{t('overview.quickSettings')}</ButtonLink>
    {/if}
  </div>
</section>

<style>
  .ov-quick {
    margin-bottom: var(--row-gap);
  }
  .ov-section-h {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 16px;
    letter-spacing: -0.01em;
    color: var(--bb-white);
    margin: 0 0 12px;
  }
  .ov-quick__row {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
  }
  .ov-quick__row :global(.ov-quick__btn) {
    min-height: 44px;
  }
  @media (max-width: 480px) {
    .ov-quick__row :global(.ov-quick__btn) {
      flex: 1 1 100%;
    }
  }
</style>
