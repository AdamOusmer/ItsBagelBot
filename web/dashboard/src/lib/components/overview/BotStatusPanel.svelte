<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { ConnUi } from '@bagel/kit/connection-state';
  import { statusTone } from '@bagel/kit/status-tone';

  const { t } = getI18n();

  let {
    loading = false,
    ui,
    checkingText,
    busy = false,
    isDelegate = false,
    isPremium = false,
    logoSrc,
    planLabel,
    onRestart,
    onDisconnect,
    enableSubmit
  }: {
    loading?: boolean;
    ui?: ConnUi;
    checkingText: string;
    busy?: boolean;
    isDelegate?: boolean;
    isPremium?: boolean;
    logoSrc: string;
    planLabel?: string;
    onRestart?: () => void;
    onDisconnect?: () => void;
    enableSubmit?: SubmitFunction;
  } = $props();

  const kind = $derived(ui?.kind ?? 'online');
  const tone = $derived(statusTone(kind));
  const live = $derived(kind === 'online');

  const strip = $derived(!loading && kind === 'online');

  const title = $derived.by(() => {
    switch (kind) {
      case 'online':
        return t('overview.onlineInChat');
      case 'connecting':
        return t('overview.connecting');
      case 'degraded':
        return t('overview.reconnectNeeded');
      case 'reauth_required':
        return t('overview.twitchAccessLost');
      case 'bot_banned':
        return t('overview.botBanned');
      case 'sub_unknown':
        return t('overview.connectedIdle');
      case 'unavailable':
        return t('overview.unavailable');
      default:
        return t('overview.notConnected');
    }
  });

  const detail = $derived.by(() => {
    switch (kind) {
      case 'online':
        return t('overview.allGood');
      case 'degraded':
        return t('overview.issueSubs');
      case 'reauth_required':
        return t('overview.issueReauth');
      case 'bot_banned':
        return t('overview.issueBotBanned');
      case 'sub_unknown':
        return t('overview.issueIdle');
      case 'disabled':
        return t('overview.statusPausedDetail');
      case 'auth_required':
        return t('overview.issueNoAuth');
      case 'unavailable':
        return t('overview.commandsUnavailableDesc');
      default:
        return '';
    }
  });
</script>

{#if strip}
  <Card as="section" sheen class="ov-status ov-status--strip" aria-label={t('overview.statusHeading')}>
    <i class="bb-mark ov-strip__dot" class:bb-mark--hollow={!live} aria-hidden="true"></i>
    <span class="ov-strip__title">{title}</span>
    {#if planLabel}<span class="ov-strip__meta">{planLabel}</span>{/if}
    <div class="ov-strip__spacer"></div>
    {#if isDelegate}
      <p class="ov-status__note">{t('overview.statusDelegateDetail')}</p>
    {:else if ui?.canManage}
      <button type="button" class="bb-chip bb-chip--muted" disabled={busy} onclick={() => onRestart?.()}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" />
        </svg>
        {t('overview.restart')}
      </button>
      <button type="button" class="bb-chip bb-chip--muted" disabled={busy} onclick={() => onDisconnect?.()}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M18.4 5.6a9 9 0 1 1-12.8 0" />
          <line x1="12" y1="2" x2="12" y2="12" />
        </svg>
        {t('overview.disconnect')}
      </button>
    {/if}
  </Card>
{:else}
  <Card as="section" sheen class="ov-status {isPremium ? 'ov-status--premium' : ''}" aria-labelledby="ov-status-h">
    <div class="ov-status__mark"><img src={logoSrc} alt="" /></div>

    <div class="ov-status__body">
      <h2 id="ov-status-h" class="ov-status__heading">{t('overview.statusHeading')}</h2>

      {#if loading}
        <p class="ov-status__state" aria-busy="true">
          <span class="bb-sr-only">{checkingText}</span>
          <span aria-hidden="true"><Skeleton variant="text" width="14ch" /></span>
        </p>
        <p class="ov-status__detail" aria-hidden="true"><Skeleton variant="text" lines={2} width="90%" /></p>
      {:else}
        <p class="ov-status__state tone-{tone}">
          <i class="bb-mark dot" class:bb-mark--hollow={!live} aria-hidden="true"></i>
          <span class="state-text">{title}</span>
        </p>
        {#if detail}<p class="ov-status__detail">{detail}</p>{/if}

        {#if planLabel}
          <div class="ov-status__meta">
            <Tag tone={isPremium ? 'pre' : 'quiet'}>{planLabel}</Tag>
          </div>
        {/if}
      {/if}
    </div>

    {#if !loading && ui}
      <div class="ov-status__actions">
        {#if isDelegate}
          <p class="ov-status__note">{t('overview.statusDelegateDetail')}</p>
        {:else if ui.canManage}
          <Button
            variant={kind === 'degraded' ? 'primary' : 'ghost'}
            type="button"
            class="ov-cta"
            disabled={busy}
            onclick={() => onRestart?.()}
          >{kind === 'degraded' ? t('common.reconnect') : t('overview.restart')}</Button>
          <Button variant="ghost" type="button" class="ov-cta" disabled={busy} onclick={() => onDisconnect?.()}>{t('overview.disconnect')}</Button>
        {:else if ui.showEnable}
          <form method="POST" action="?/enable" use:enhance={enableSubmit}>
            <Button variant="primary" type="submit" class="ov-cta" loading={busy}>{t('overview.enable')}</Button>
          </form>
        {:else if ui.showConnect}
          <ButtonLink href="/settings" variant="primary" class="ov-cta"
            >{kind === 'reauth_required' ? t('common.reconnect') : t('overview.issueNoAuthCta')}</ButtonLink>
        {:else if ui.canRetry}
          <ButtonLink href="/" variant="ghost" class="ov-cta">{t('overview.retry')}</ButtonLink>
        {/if}
      </div>
    {/if}
  </Card>
{/if}

<style>
  :global(.ov-status) {
    margin-bottom: var(--row-gap);
  }
  :global(.ov-status:not(.ov-status--strip)) {
    display: grid;
    grid-template-columns: auto 1fr auto;
    gap: 22px;
    align-items: center;
  }
  :global(.ov-status--premium) {
    border-color: rgba(201, 168, 124, 0.4);
  }

  :global(.ov-status--strip) {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 12px;
  }
  .ov-strip__dot {
    width: 7px;
    height: 7px;
    color: var(--bb-status-success);
  }
  .ov-strip__title {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 16px;
    letter-spacing: -0.01em;
    color: var(--bb-white);
  }
  .ov-strip__meta {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .ov-strip__spacer {
    flex: 1 1 auto;
    min-width: 8px;
  }

  .ov-status__mark {
    width: 58px;
    height: 58px;
    border-radius: 50%;
    background: rgba(82, 183, 136, 0.07);
    border: 1px solid rgba(82, 183, 136, 0.3);
    display: flex;
    align-items: center;
    justify-content: center;
    flex: none;
  }
  :global(.ov-status--premium) .ov-status__mark {
    border-color: rgba(201, 168, 124, 0.4);
    background: rgba(201, 168, 124, 0.05);
  }
  .ov-status__mark img {
    width: 38px;
    height: 38px;
    border-radius: 50%;
  }

  .ov-status__body {
    min-width: 0;
  }
  .ov-status__heading {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    font-weight: 500;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--bb-muted);
    margin: 0 0 8px;
  }
  .ov-status__state {
    display: flex;
    align-items: center;
    gap: 12px;
    margin: 0;
    font-family: var(--bb-font-display);
    font-weight: 800;
    font-size: clamp(22px, 2.4vw, 28px);
    letter-spacing: -0.01em;
    line-height: 1.1;
    color: var(--bb-white);
  }
  .tone-success .state-text {
    color: var(--bb-white);
  }
  .tone-error .state-text {
    color: var(--bb-status-error-fg);
  }
  .tone-warning .state-text {
    color: var(--bb-status-warning-fg);
  }
  .dot {
    width: 8px;
    height: 8px;
    color: var(--bb-muted);
  }
  .tone-success .dot {
    color: var(--bb-status-success);
  }
  .tone-error .dot {
    color: var(--bb-status-error);
  }
  .tone-warning .dot {
    color: var(--bb-status-warning);
  }

  .ov-status__detail {
    margin: 8px 0 0;
    max-width: 46ch;
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    line-height: 1.5;
    color: var(--bb-muted);
  }
  .ov-status__meta {
    margin-top: 12px;
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
  .ov-status__actions {
    display: flex;
    gap: 10px;
    align-items: center;
    flex: none;
  }
  .ov-status__actions :global(.ov-cta) {
    min-height: 44px;
  }
  .ov-status__note {
    margin: 0;
    max-width: 30ch;
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    line-height: 1.45;
    color: var(--bb-muted);
    text-align: right;
  }

  @media (max-width: 760px) {
    :global(.ov-status:not(.ov-status--strip)) {
      grid-template-columns: auto 1fr;
      gap: 16px;
    }
    .ov-status__actions {
      grid-column: 1 / -1;
      flex-direction: column;
      align-items: stretch;
    }
    .ov-status__actions :global(.ov-cta),
    .ov-status__actions form {
      width: 100%;
    }
    .ov-status__actions form :global(.ov-cta) {
      width: 100%;
    }
    .ov-status__note {
      text-align: left;
      max-width: none;
    }
  }
</style>
