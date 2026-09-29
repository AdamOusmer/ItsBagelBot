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
    actionPayload,
    getI18n,
    toast,
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
        failed(payload ?? undefined, 'spotify.appSaveFailed');
        return;
      }
      clientSecret = '';
      replacing = false;
      toast('ok', t('spotify.appSaved'));
      await invalidateAll();
    };
  };

  async function copyRedirect() {
    const copied = await copyText(redirectUri);
    toast(copied ? 'ok' : 'err', t(copied ? 'spotify.redirectCopied' : 'spotify.redirectCopyFailed'));
  }
</script>

<Card>
  <div class="head"><Heading level={6} as="h2">{t('spotify.connectionTitle')}</Heading></div>

  <div class="line">
    <span class="line-label"><Text as="span" size="sm" tone="muted">{t('spotify.accountLabel')}</Text></span>
    <Tag tone={needsReconnect ? 'error' : 'live'} mark={needsReconnect ? 'hollow' : 'solid'}>
      {needsReconnect ? t('spotify.needsReconnectPill') : t('spotify.connectedPill')}
    </Tag>
    <span class="line-actions">
      <ButtonLink variant="secondary" href="/spotify/connect" data-sveltekit-reload>{t('spotify.reconnectSpotify')}</ButtonLink>
      <Button variant="destructive" type="button" onclick={onDisconnect}>{t('spotify.disconnect')}</Button>
    </span>
  </div>

  {#if needsReconnect}
    <div class="alert-slot"><AlertBanner variant="warn">
      {t(grantRevoked ? 'spotify.grantRevoked' : 'spotify.scopeGap')}
      {#snippet action()}
        <ButtonLink variant="primary" href="/spotify/connect" data-sveltekit-reload>{t('spotify.reconnectSpotify')}</ButtonLink>
      {/snippet}
    </AlertBanner></div>
  {/if}

  <div class="line">
    <span class="line-label"><Text as="span" size="sm" tone="muted">{t('spotify.appLabel')}</Text></span>
    <Code>{app.clientId}</Code>
    <span class="line-actions">
      <Button variant="secondary" type="button" onclick={startReplace} disabled={replacing}>{t('spotify.appReplace')}</Button>
      <Button variant="destructive" type="button" onclick={onRemoveApp}>{t('spotify.appRemove')}</Button>
    </span>
  </div>

  {#if replacing}
    <form method="POST" action="?/saveApp" use:enhance={appSubmit} class="replace">
      <Text size="sm" tone="muted">
        {t('spotify.appStepRedirect')}
        <Code>{redirectUri}</Code>
        <Button variant="ghost" type="button" onclick={copyRedirect}>{t('spotify.redirectCopy')}</Button>
      </Text>
      <Field label={t('spotify.appClientIdLabel')} hint={t('spotify.appClientIdHint')}>
        <Input fill name="client_id" bind:value={clientId} autocomplete="off" spellcheck="false" required />
      </Field>
      <Field label={t('spotify.appClientSecretLabel')} hint={t('spotify.appClientSecretHint')}>
        <Input fill name="client_secret" type="password" bind:value={clientSecret} autocomplete="off" spellcheck="false" required />
      </Field>
      <div class="line-actions">
        <Button variant="primary" type="submit" loading={saving}>{t('spotify.appSave')}</Button>
        <Button variant="ghost" type="button" onclick={cancelReplace} disabled={saving}>{t('spotify.appCancel')}</Button>
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
