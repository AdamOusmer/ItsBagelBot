<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { copyText } from '@bagel/ui/lib/clipboard';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import {
    AlertBanner,
    Button,
    ButtonLink,
    Card,
    Code,
    Field,
    Heading,
    Tag,
    Text,
    toast
  } from '@bagel/ui/svelte';
  import {
    actionPayload,
    getI18n,
    toastFailure,
    type ActionOk
  } from '@bagel/kit';

  let {
    app,
    scopeGap,
    grantRevoked = false,
    redirectUri,
    onRemoveApp,
    onDisconnect
  }: {
    app: { present: boolean; clientId: string };
    scopeGap: string[];
    grantRevoked?: boolean;
    redirectUri: string;
    onRemoveApp: () => void;
    onDisconnect: () => void;
  } = $props();

  const { t } = getI18n();
  const failed = toastFailure(toast, t);

  let replacing = $state(false);
  let saving = $state(false);
  let clientId = $state('');
  let clientSecret = $state('');
  const needsReconnect = $derived(scopeGap.length > 0 || grantRevoked);

  function startReplace() {
    clientId = app.clientId;
    clientSecret = '';
    replacing = true;
  }
  function cancelReplace() {
    clientSecret = '';
    replacing = false;
  }

  const appSubmit: SubmitFunction = () => {
    saving = true;
    return async ({ result }) => {
      saving = false;
      const payload = actionPayload<ActionOk>(result);
      if (result.type !== 'success' || payload?.ok === false) {
        failed(payload ?? undefined, 'spotify.app.saveFailed');
        return;
      }
      clientSecret = '';
      replacing = false;
      toast('ok', t('spotify.app.saved'));
      await invalidateAll();
    };
  };

  async function copyRedirect() {
    const copied = await copyText(redirectUri);
    toast(copied ? 'ok' : 'err', t(copied ? 'spotify.app.redirectCopied' : 'spotify.app.redirectCopyFailed'));
  }
</script>

<Card>
  <div class="head"><Heading level={6} as="h2">{t('spotify.connection.title')}</Heading></div>

  <div class="line">
    <span class="line-label"><Text as="span" size="sm" tone="muted">{t('spotify.connection.accountLabel')}</Text></span>
    <Tag tone={needsReconnect ? 'error' : 'live'} mark={needsReconnect ? 'hollow' : 'solid'}>
      {needsReconnect ? t('spotify.connection.needsReconnectPill') : t('spotify.connection.connectedPill')}
    </Tag>
    <span class="line-actions">
      <ButtonLink variant="secondary" href="/spotify/connect" data-sveltekit-reload>{t('spotify.connection.reconnectSpotify')}</ButtonLink>
      <Button variant="destructive" type="button" onclick={onDisconnect}>{t('spotify.connection.disconnect')}</Button>
    </span>
  </div>

  {#if needsReconnect}
    <div class="alert-slot"><AlertBanner tone="warning">
      {t(grantRevoked ? 'spotify.connection.grantRevoked' : 'spotify.connection.scopeGap')}
      {#snippet actions()}
        <ButtonLink variant="primary" href="/spotify/connect" data-sveltekit-reload>{t('spotify.connection.reconnectSpotify')}</ButtonLink>
      {/snippet}
    </AlertBanner></div>
  {/if}

  <div class="line">
    <span class="line-label"><Text as="span" size="sm" tone="muted">{t('spotify.app.label')}</Text></span>
    <Code>{app.clientId}</Code>
    <span class="line-actions">
      <Button variant="secondary" type="button" onclick={startReplace} disabled={replacing}>{t('spotify.app.replace')}</Button>
      <Button variant="destructive" type="button" onclick={onRemoveApp}>{t('spotify.app.remove')}</Button>
    </span>
  </div>

  {#if replacing}
    <form method="POST" action="?/saveApp" use:enhance={appSubmit} class="replace">
      <Text size="sm" tone="muted">
        {t('spotify.app.stepRedirect')}
        <Code>{redirectUri}</Code>
        <Button variant="ghost" type="button" onclick={copyRedirect}>{t('spotify.app.redirectCopy')}</Button>
      </Text>
      <Field label={t('spotify.app.clientIdLabel')} hint={t('spotify.app.clientIdHint')}>
        <Input fill name="client_id" bind:value={clientId} autocomplete="off" spellcheck="false" required />
      </Field>
      <Field label={t('spotify.app.clientSecretLabel')} hint={t('spotify.app.clientSecretHint')}>
        <Input fill name="client_secret" type="password" bind:value={clientSecret} autocomplete="off" spellcheck="false" required />
      </Field>
      <div class="line-actions">
        <Button variant="primary" type="submit" loading={saving}>{t('spotify.app.save')}</Button>
        <Button variant="ghost" type="button" onclick={cancelReplace} disabled={saving}>{t('spotify.app.cancel')}</Button>
      </div>
    </form>
  {/if}
</Card>

<style>
  .head { margin-bottom: 10px; }
  .line { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; min-height: 44px; }
  .line + .line, .alert-slot { margin-top: 8px; }
  .line-label { min-width: 110px; }
  .line-actions { display: flex; gap: 10px; flex-wrap: wrap; margin-left: auto; }
  .replace { display: grid; gap: 12px; margin-top: 14px; }
</style>
