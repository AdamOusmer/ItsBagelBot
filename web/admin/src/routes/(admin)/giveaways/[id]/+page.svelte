<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit/i18n/context';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import CampaignDetails from '#lib/components/giveaways/CampaignDetails.svelte';

  let { data, form } = $props();
  const { t } = getI18n();
</script>

{#snippet back()}
  <TextLink variant="quiet" href="/giveaways" icon="arrowLeft" touch>{t('admin.giveaways.back')}</TextLink>
{/snippet}

<section class="screen active">
  {#if data.giveaway}
    {#key data.id}
      <CampaignDetails giveaway={data.giveaway} alerts={data.alerts} result={form} />
    {/key}
  {:else}
    <PageHead title={t('admin.giveaways.detail')} />
    <PageToolbar leading={back} />
    <AlertBanner tone="danger">{t('admin.giveaways.unavailable')}</AlertBanner>
  {/if}
</section>
