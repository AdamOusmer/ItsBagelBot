<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import CampaignDraft from '#lib/components/campaigns/CampaignDraft.svelte';
  import CampaignHistory from '#lib/components/campaigns/CampaignHistory.svelte';

  let { data, form } = $props();
  const { t } = getI18n();
</script>

<section class="screen active">
  <PageHead description={t('admin.campaigns.description')}>
    {t('admin.campaigns.titlePre')}<em>{t('admin.campaigns.titleEm')}</em>
  </PageHead>

  <Stack gap={5}>
    <CampaignHistory history={data.history} />
    <CampaignDraft result={form} />
  </Stack>
</section>
