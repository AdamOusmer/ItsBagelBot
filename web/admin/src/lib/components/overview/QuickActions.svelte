<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';

  let { canNotify }: { canNotify: boolean } = $props();

  const { t } = getI18n();

  let q = $state('');
</script>

<Card as="section">
  <CardHead title={t('admin.overview.quickTitle')} />

  <Stack gap={3}>
    <Cluster as="form" gap={2} nowrap method="GET" action="/users">
      <SearchInput fill bind:value={q} placeholder={t('admin.overview.quickLookupPlaceholder')} />
      <input type="hidden" name="q" value={q} />
      <Button variant="ghost" type="submit">{t('admin.overview.quickLookupCta')}</Button>
    </Cluster>

    {#if canNotify}
      <Cluster gap={2}>
        <ButtonLink variant="ghost" href="/notifications">
          {t('admin.overview.quickNotifications')}
        </ButtonLink>
      </Cluster>
    {/if}
  </Stack>
</Card>
