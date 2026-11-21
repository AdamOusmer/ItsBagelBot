<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import StatusStrip from '#lib/components/deploys/StatusStrip.svelte';
  import RunHistory from '#lib/components/deploys/RunHistory.svelte';
  import FluxPanel from '#lib/components/deploys/FluxPanel.svelte';

  let { data } = $props();
  const { t } = getI18n();
</script>

<section class="screen active">
  <PageHead compact description={t('admin.deploys.description')}>
    {t('admin.deploys.titlePre')}<em>{t('admin.deploys.titleEm')}</em>
  </PageHead>

  <Stack gap={4}>
    <StatusStrip planned={data.planned} runs={data.runs} />
    <RunHistory result={data.runs} />

    <div class="cluster-slot">
      {#await data.flux}
        <div class="cluster-loading" aria-busy="true" aria-label={t('admin.deploys.flux.title')} role="region">
          <Skeleton variant="block" height="100%" />
        </div>
      {:then flux}
        <FluxPanel {flux} />
      {:catch}
        <AlertBanner tone="danger">{t('admin.deploys.flux.unavailable')}</AlertBanner>
      {/await}
    </div>
  </Stack>
</section>

<style>
  .cluster-slot {
    display: grid;
    grid-template: minmax(0, 1fr) / minmax(0, 1fr);
    block-size: 35rem;
  }

  .cluster-loading {
    display: grid;
  }

  @media (max-width: 42rem) {
    .cluster-slot {
      block-size: 30rem;
    }
  }
</style>
