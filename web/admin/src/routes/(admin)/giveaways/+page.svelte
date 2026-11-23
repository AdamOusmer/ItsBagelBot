<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import GiveawayDraft from '#lib/components/giveaways/GiveawayDraft.svelte';
  import GiveawayHistory from '#lib/components/giveaways/GiveawayHistory.svelte';

  let { data, form } = $props();
  const { t } = getI18n();
</script>

<section class="screen active">
  <PageHead description={t('admin.giveaways.description')}>
    {t('admin.giveaways.titlePre')}<em>{t('admin.giveaways.titleEm')}</em>
  </PageHead>

  <Stack gap={5}>
    <div><GiveawayHistory history={data.history} /></div>
    <GiveawayDraft result={form} />
  </Stack>
</section>
