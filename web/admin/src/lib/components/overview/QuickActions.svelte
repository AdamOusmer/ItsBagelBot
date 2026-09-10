<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The two jumps an operator makes from this page: find a specific broadcaster,
  // or tell everyone something. The lookup is a real GET form so it works
  // without JS and lands on /users with the query already applied -- the users
  // page owns the search, this only aims it.
  import Card from '@bagel/shared/components/Card.svelte';
  import CardHead from '@bagel/shared/components/CardHead.svelte';
  import Button from '@bagel/shared/components/Button.svelte';
  import ButtonLink from '@bagel/shared/components/ButtonLink.svelte';
  import SearchInput from '@bagel/shared/components/SearchInput.svelte';
  import { getI18n } from '@bagel/shared/i18n/context';

  let { canNotify }: { canNotify: boolean } = $props();

  const { t } = getI18n();

  let q = $state('');
</script>

<Card as="section">
  <CardHead title={t('admin.overview.quickTitle')} />

  <form class="lookup" method="GET" action="/users">
    <!-- SearchInput is itself a <label> wrapping an unnamed input, so the value
         is carried by a hidden field rather than by naming that input: nesting
         a second label around it would be invalid. -->
    <SearchInput bind:value={q} placeholder={t('admin.overview.quickLookupPlaceholder')} />
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
  .lookup :global(.search) {
    flex: 1;
    min-width: 0;
  }
  .jumps {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    margin-top: 12px;
  }
</style>
