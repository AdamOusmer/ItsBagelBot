<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import '@bagel/kit/styles/ui-console.css';
  import { getI18n } from '@bagel/kit';
  import { page } from '$app/state';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';

  const { t } = getI18n();
  let { data, form } = $props();

  function platformLabel(p: string): string {
    if (p === 'macos') return t('oauthDesktop.platformMacos');
    if (p === 'windows') return t('oauthDesktop.platformWindows');
    if (p === 'linux') return t('oauthDesktop.platformLinux');
    return p;
  }

  // Without JS, a plain form POST re-renders this page via `load()` against the action's own
  // URL: `?/approve` alone would replace the whole query string and drop every OAuth param, so
  // the request the load re-validates against has to keep carrying them.
  function actionUrl(name: string): string {
    return page.url.search ? `${page.url.search}&/${name}` : `?/${name}`;
  }
</script>

<svelte:head>
  <title>{t('oauthDesktop.title')}</title>
  <meta name="referrer" content="no-referrer" />
  {#if form?.target}<meta http-equiv="refresh" content={`0;url=${form.target}`} />{/if}
</svelte:head>

<section class="screen active oauth-desktop">
  {#if form?.target}
    <Card>
      <Text as="p">
        {form.action === 'approved' ? t('oauthDesktop.approvedRedirecting') : t('oauthDesktop.cancelledRedirecting')}
      </Text>
      <Text as="p" size="sm" tone="muted">{t('oauthDesktop.closeWindow')}</Text>
    </Card>
  {:else if data.refusal}
    <PageHead eyebrow={t('oauthDesktop.eyebrow')} title={t('oauthDesktop.title')} />
    <AlertBanner tone="warning" role="status">
      {data.refusal === 'impersonator' ? t('oauthDesktop.refusalImpersonator') : t('oauthDesktop.refusalDelegate')}
    </AlertBanner>
  {:else}
    <PageHead eyebrow={t('oauthDesktop.eyebrow')} title={t('oauthDesktop.title')} />
    {#if form?.error}<AlertBanner tone="danger" role="alert">{form.error}</AlertBanner>{/if}
    <Card>
      <Text as="p">{t('oauthDesktop.asAccount', { name: data.account.displayName || data.account.login })}</Text>

      <dl class="oauth-desktop-device">
        <div class="oauth-desktop-device-row">
          <dt><Text as="span" size="sm" tone="muted">{t('oauthDesktop.deviceLabel')}</Text></dt>
          <dd><Text as="span">{data.request.deviceName || t('oauthDesktop.deviceUnnamed')}</Text></dd>
        </div>
        <div class="oauth-desktop-device-row">
          <dt><Text as="span" size="sm" tone="muted">{t('oauthDesktop.platformLabel')}</Text></dt>
          <dd><Text as="span">{platformLabel(data.request.platform)}</Text></dd>
        </div>
      </dl>

      <Text as="p" size="sm" tone="muted">{t('oauthDesktop.reportedByApp')}</Text>
      <Text as="p" size="sm">{t('oauthDesktop.accessGranted')}</Text>

      <AlertBanner tone="warning" role="note">{t('oauthDesktop.onlyIfYouStarted')}</AlertBanner>

      <div class="oauth-desktop-actions">
        <form method="POST" action={actionUrl('cancel')}>
          {@render hiddenFields()}
          <Button type="submit" variant="ghost">{t('oauthDesktop.cancel')}</Button>
        </form>
        <form method="POST" action={actionUrl('approve')}>
          {@render hiddenFields()}
          <Button type="submit" variant="primary">{t('oauthDesktop.approve')}</Button>
        </form>
      </div>
    </Card>
  {/if}
</section>

{#snippet hiddenFields()}
  <input type="hidden" name="response_type" value="code" />
  <input type="hidden" name="client_id" value={data.request.clientId} />
  <input type="hidden" name="redirect_uri" value={data.request.redirectUri} />
  <input type="hidden" name="code_challenge" value={data.request.codeChallenge} />
  <input type="hidden" name="code_challenge_method" value="S256" />
  <input type="hidden" name="dpop_jkt" value={data.request.dpopJkt} />
  <input type="hidden" name="state" value={data.request.state} />
  <input type="hidden" name="device_name" value={data.request.deviceName} />
  <input type="hidden" name="platform" value={data.request.platform} />
{/snippet}

<style>
  .oauth-desktop {
    max-width: 480px;
    margin: 48px auto;
    padding: 0 16px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .oauth-desktop-device {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin: 0;
    padding: 12px 0;
  }

  .oauth-desktop-device-row {
    display: flex;
    justify-content: space-between;
    gap: 12px;
  }

  .oauth-desktop-device-row dt,
  .oauth-desktop-device-row dd {
    margin: 0;
  }

  .oauth-desktop-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    flex-wrap: wrap;
  }
</style>
