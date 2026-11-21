<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';

  const { t } = getI18n();
</script>

<section class="screen active">
  <PageHead compact>{t('admin.deploys.runEyebrow')}</PageHead>
  <PageToolbar>
    {#snippet trailing()}
      <ButtonLink href="/deploys" variant="secondary">{t('admin.deploys.back')}</ButtonLink>
    {/snippet}
  </PageToolbar>
  <AlertBanner>{t('admin.deploys.run.loadFailed')}</AlertBanner>
</section>
