<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';

  let { canNotify }: { canNotify: boolean } = $props();

  const { t } = getI18n();

  let q = $state('');
</script>

<Card as="section">
  <CardHead title={t('admin.overview.quickTitle')} />

  <form class="lookup" method="GET" action="/users">
    <SearchInput fill bind:value={q} placeholder={t('admin.overview.quickLookupPlaceholder')} />
    <input type="hidden" name="q" value={q} />
    <Button variant="ghost" type="submit">{t('admin.overview.quickLookupCta')}</Button>
  </form>

  {#if canNotify}
    <div class="jumps">
      <ButtonLink variant="ghost" href="/notifications">
        {t('admin.overview.quickNotifications')}
      </ButtonLink>
    </div>
  {/if}
</Card>

<style>
  .lookup {
    display: flex;
    gap: 8px;
    align-items: center;
  }
  
  .jumps {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    margin-top: 12px;
  }
</style>
