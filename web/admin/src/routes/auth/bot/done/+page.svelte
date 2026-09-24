<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { page } from '$app/state';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Container from '@bagel/ui/svelte/Container.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';

  const { t } = getI18n();

  const MESSAGES = {
    state: 'admin.botAuth.errState',
    oauth: 'admin.botAuth.errOauth',
    account: 'admin.botAuth.errAccount',
    config: 'admin.botAuth.errConfig'
  } as const;

  const ok = $derived(page.url.searchParams.get('ok') === '1');
  const err = $derived(page.url.searchParams.get('e') ?? '');
  const reasonKey = $derived(MESSAGES[err as keyof typeof MESSAGES]);
</script>

<Container as="main" class="done">
  <Stack gap={2} align="center">
    {#if ok}
      <Heading level={1} variant="card">{t('admin.botAuth.okTitle')}</Heading>
      <Text tone="muted">{t('admin.botAuth.okBody')}</Text>
    {:else}
      <Heading level={1} variant="card">{t('admin.botAuth.failTitle')}</Heading>
      <Text tone="muted">{reasonKey ? t(reasonKey) : t('admin.botAuth.errGeneric')}</Text>
    {/if}
  </Stack>
</Container>

<style>
  :global(main.done) {
    --container-max: 460px;
    margin-block: 18vh;
    text-align: center;
  }
</style>
