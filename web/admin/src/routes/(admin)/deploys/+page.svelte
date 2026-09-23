<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  // The release train, owner only. The deployer service runs every stage;
  // this page reads the plan, starts a run and lists past ones. The live view
  // of a run is /deploys/<id>, which ?/start redirects to.
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import StatusStrip from '$lib/components/deploys/StatusStrip.svelte';
  import ShipPanel from '$lib/components/deploys/ShipPanel.svelte';
  import RunHistory from '$lib/components/deploys/RunHistory.svelte';

  let { data } = $props();

  const { t } = getI18n();

  // The active run is also the newest one, so "last run" means the last one
  // that is not the run already named in the cluster tile.
  const last = $derived(data.runs.find((r) => r.id !== data.active?.id) ?? null);
</script>

<section class="screen active">
  <PageHead eyebrow={t('admin.deploys.eyebrow')} description={t('admin.deploys.description')}>
    {t('admin.deploys.titlePre')}<em>{t('admin.deploys.titleEm')}</em>
  </PageHead>

  <StatusStrip plan={data.plan} active={data.active} {last} />

  {#if data.planError}
    <AlertBanner>{t('admin.deploys.planError', { error: data.planError })}</AlertBanner>
  {/if}

  <div class="stack">
    <ShipPanel plan={data.plan} active={data.active} />
    <RunHistory runs={data.runs} />
  </div>
</section>

<style>
  .stack {
    display: flex;
    flex-direction: column;
    gap: 16px;
    margin-top: 16px;
  }
</style>
