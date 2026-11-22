<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { getI18n } from '@bagel/kit/i18n/context';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageNotice from '#lib/components/shared/PageNotice.svelte';
  import PremiumGate from '#lib/components/shared/PremiumGate.svelte';
  import { premiumLocked } from '#lib/components/shared/premium-lock';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();
  const { t } = getI18n();
</script>

<svelte:head><title>{t('overlays.customTitle')}</title></svelte:head>

{#if premiumLocked(data)}
  <PremiumGate />
{:else}
  <section class="screen active">
    <PageHead>
      <bb-fit class="page-title"><span data-fit="">{t('overlays.customTitle')}</span></bb-fit>
    </PageHead>
    <PageNotice tone="info" role="note">{t('overlays.customSoon')}</PageNotice>
    <ButtonLink variant="secondary" href="/overlays">{t('overlays.customBack')}</ButtonLink>
  </section>
{/if}

<style>
  .page-title { min-inline-size: 0; }
</style>
