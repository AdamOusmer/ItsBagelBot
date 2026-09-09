<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The landing page the Twitch bot-account consent flow redirects back to. It
  // is outside the (admin) group on purpose: the operator arrives here in
  // whatever tab Twitch opened, which may not carry an admin session, so this
  // page renders an outcome and nothing else.
  import { page } from '$app/state';
  import { getI18n } from '@bagel/shared/i18n/context';

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

<main>
  {#if ok}
    <h1>{t('admin.botAuth.okTitle')}</h1>
    <p>{t('admin.botAuth.okBody')}</p>
  {:else}
    <h1>{t('admin.botAuth.failTitle')}</h1>
    <p>{reasonKey ? t(reasonKey) : t('admin.botAuth.errGeneric')}</p>
  {/if}
</main>

<style>
  main {
    max-width: 460px;
    margin: 18vh auto;
    padding: 0 24px;
    font-family: var(--bb-font-body, system-ui, sans-serif);
    text-align: center;
  }
  h1 {
    font-size: 1.4rem;
    margin: 0 0 8px;
  }
  p {
    color: var(--bb-muted, #888);
    margin: 0;
  }
</style>
