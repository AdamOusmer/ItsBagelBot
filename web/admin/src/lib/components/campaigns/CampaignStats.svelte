<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit/i18n/context';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import { SEND_LABEL, SEND_STATUSES } from '#lib/campaign-workflow';
  import type { CampaignAudience, CampaignWire } from '#lib/server/campaigns';

  let { campaign, audience }: { campaign: CampaignWire; audience: Promise<CampaignAudience | null> } = $props();

  const { locale, t } = getI18n();
  const number = new Intl.NumberFormat(locale);
</script>

<Grid min="25rem" gap={4}>
  <Card as="section">
    <Stack gap={3}>
      <CardHead title={t('admin.campaigns.eligibleTitle')} headingLevel={2} />
      {#await audience}
        <Skeleton width="60%" lines={2} />
      {:then counts}
        {#if counts}
          <FactList layout="tiles">
            {#each Object.entries(counts.eligible) as [code, count] (code)}
              <Fact term={code.toUpperCase()}>{number.format(count)}</Fact>
            {/each}
            <Fact term={t('admin.campaigns.eligibleTotal')} wide>{number.format(counts.total)}</Fact>
          </FactList>
        {:else}
          <AlertBanner tone="warning" flush>{t('admin.campaigns.eligibleUnavailable')}</AlertBanner>
        {/if}
      {/await}
    </Stack>
  </Card>

  <Card as="section">
    <Stack gap={3}>
      <CardHead title={t('admin.campaigns.statsTitle')} headingLevel={2} />
      <FactList layout="tiles">
        {#each SEND_STATUSES as status (status)}
          <Fact term={t(SEND_LABEL[status])}>{number.format(campaign.sends?.[status] ?? 0)}</Fact>
        {/each}
      </FactList>
    </Stack>
  </Card>
</Grid>
