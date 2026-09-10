<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The landing page the Twitch bot-account consent flow redirects back to. It
  // is outside the (admin) group on purpose: the operator arrives here in
  // whatever tab Twitch opened, which may not carry an admin session, so this
  // page renders an outcome and nothing else.
  import { page } from '$app/state';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Container from '@bagel/ui/svelte/Container.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';

  const { t } = getI18n();

  // Written by the callback in $lib/server/oauth. A table rather than a chain,
  // for the same reason the sign-in page's is one: an unrecognised reason must
  // fall through to the generic line, not to the wrong specific one.
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
  /* Page-only composition: where this one card of copy sits in an otherwise
     empty viewport. The type is Heading/Text and the measure and gutter are
     Container's, so nothing here restates the scale.

     `--container-max` rather than a `max-width`: 460px is narrower than any of
     the three named container widths, because this page is two sentences and
     a title, and Container exposes the knob for exactly this. */
  :global(main.done) {
    --container-max: 460px;
    margin-block: 18vh;
    text-align: center;
  }
</style>
