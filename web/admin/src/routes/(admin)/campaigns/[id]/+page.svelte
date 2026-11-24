<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit/i18n/context';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import CampaignEditor from '#lib/components/campaigns/CampaignEditor.svelte';

  let { data, form } = $props();
  const { t } = getI18n();
</script>

{#snippet back()}
  <TextLink variant="quiet" href="/campaigns" icon="arrowLeft" touch>{t('admin.campaigns.back')}</TextLink>
{/snippet}

<section class="screen active">
  {#if data.campaign}
    {#key data.id}
      <CampaignEditor campaign={data.campaign} audience={data.audience} canApprove={data.canApprove} result={form} />
    {/key}
  {:else}
    <PageHead title={t('admin.campaigns.history')} />
    <PageToolbar leading={back} />
    <AlertBanner tone="danger">{t('admin.campaigns.unavailable')}</AlertBanner>
  {/if}
</section>
