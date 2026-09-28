<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import RetryButton from './RetryButton.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import Mark from '@bagel/ui/svelte/Mark.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import VisuallyHidden from '@bagel/ui/svelte/VisuallyHidden.svelte';
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

  const strip = $derived(kind === 'online');

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

<VisuallyHidden role="status" aria-live="polite">{loading ? '' : title}</VisuallyHidden>

{#if loading}
  <div class="ov-status-wrap">
    <Card as="section" sheen aria-busy="true" aria-label={t('overview.statusHeading')}>
      <div class="ov-strip">
        <VisuallyHidden>{checkingText}</VisuallyHidden>
        <span aria-hidden="true"><Skeleton variant="text" width="18ch" /></span>
        <div class="ov-strip__spacer"></div>
        <span class="ov-strip__ghost" aria-hidden="true">
          <Chip as="span" tone="muted">
            <Icon name="bolt" />
            {t('overview.restart')}
          </Chip>
          <Chip as="span" tone="muted">
            <Icon name="power" />
            {t('overview.disconnect')}
          </Chip>
        </span>
      </div>
    </Card>
  </div>
{:else if strip}
  <div class="ov-status-wrap">
    <Card as="section" sheen aria-label={t('overview.statusHeading')}>
      <div class="ov-strip">
        <span class="strip-dot"><Mark variant={live ? 'solid' : 'hollow'} size="7px" /></span>
        <Heading level={6} as="span" variant="title">{title}</Heading>
        {#if planLabel}<Label mono as="span">{planLabel}</Label>{/if}
        <div class="ov-strip__spacer"></div>
        {#if isDelegate}
          <p class="ov-status__note">{t('overview.statusDelegateDetail')}</p>
        {:else if ui?.canManage}
          <Chip tone="muted" disabled={busy} onclick={() => onRestart?.()}>
            <Icon name="bolt" />
            {t('overview.restart')}
          </Chip>
          <Chip tone="muted" disabled={busy} onclick={() => onDisconnect?.()}>
            <Icon name="power" />
            {t('overview.disconnect')}
          </Chip>
        {/if}
      </div>
    </Card>
  </div>
{:else}
  <div class="ov-status-wrap">
    <Card as="section" sheen tone={isPremium ? 'accent' : undefined} aria-labelledby="ov-status-h">
      <div class="ov-status" class:premium={isPremium}>
        <div class="ov-status__mark"><img src={logoSrc} alt="" /></div>

        <div class="ov-status__body">
          <div class="ov-status__heading">
            <Heading level={6} as="h2" variant="label" id="ov-status-h">{t('overview.statusHeading')}</Heading>
          </div>

          <p class="ov-status__state tone-{tone}">
            <span class="dot"><Mark variant={live ? 'solid' : 'hollow'} size="8px" /></span>
            <span class="state-text">{title}</span>
          </p>
          {#if detail}<p class="ov-status__detail">{detail}</p>{/if}

          {#if planLabel}
            <div class="ov-status__meta">
              <Tag tone={isPremium ? 'pre' : 'quiet'}>{planLabel}</Tag>
            </div>
          {/if}
        </div>

        {#if ui}
          <div class="ov-status__actions">
            {#if isDelegate}
              <p class="ov-status__note">{t('overview.statusDelegateDetail')}</p>
            {:else if ui.canManage}
              <Button
                variant={kind === 'degraded' ? 'primary' : 'ghost'}
                type="button"
                disabled={busy}
                onclick={() => onRestart?.()}
              >{kind === 'degraded' ? t('common.reconnect') : t('overview.restart')}</Button>
              <Button variant="ghost" type="button" disabled={busy} onclick={() => onDisconnect?.()}>{t('overview.disconnect')}</Button>
            {:else if ui.showEnable}
              <form method="POST" action="?/enable" use:enhance={enableSubmit}>
                <Button variant="primary" type="submit" loading={busy}>{t('overview.enable')}</Button>
              </form>
            {:else if ui.showConnect}
              <ButtonLink href="/settings#account" variant="primary"
                >{kind === 'reauth_required' ? t('common.reconnect') : t('overview.issueNoAuthCta')}</ButtonLink>
            {:else if ui.canRetry}
              <RetryButton />
            {/if}
          </div>
        {/if}
      </div>
    </Card>
  </div>
{/if}

<style>
  .ov-status-wrap {
    margin-bottom: var(--row-gap);
  }
  .ov-status {
    display: grid;
    grid-template-columns: auto 1fr auto;
    gap: 22px;
    align-items: center;
  }

  .ov-strip {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 12px;
  }
  .strip-dot {
    display: contents;
    color: var(--bb-status-success);
  }
  .ov-strip__ghost {
    display: contents;
    visibility: hidden;
  }
  .ov-strip__spacer {
    flex: 1 1 auto;
    min-width: 8px;
  }

  .ov-status__mark {
    width: 58px;
    height: 58px;
    border-radius: 50%;
    background: rgba(var(--bb-green-glow-rgb), 0.07);
    border: 1px solid rgba(var(--bb-green-glow-rgb), 0.3);
    display: flex;
    align-items: center;
    justify-content: center;
    flex: none;
  }
  .premium .ov-status__mark {
    border-color: rgba(var(--bb-tan-rgb), 0.4);
    background: rgba(var(--bb-tan-rgb), 0.05);
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
    margin-bottom: 8px;
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
  .dot {
    display: contents;
    color: var(--bb-muted);
  }
  .tone-success .dot {
    color: var(--bb-status-success);
  }
  .tone-error .dot {
    color: var(--bb-status-danger);
  }
  .tone-warning .dot {
    color: var(--bb-status-warning);
  }
  .tone-success .state-text {
    color: var(--bb-white);
  }
  .tone-error .state-text {
    color: var(--bb-status-danger-fg);
  }
  .tone-warning .state-text {
    color: var(--bb-status-warning-fg);
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
    --btn-min-h: 44px;
    display: flex;
    gap: 10px;
    align-items: center;
    flex: none;
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
    .ov-status {
      grid-template-columns: auto 1fr;
      gap: 16px;
    }
    .ov-status__actions {
      --btn-w: 100%;
      grid-column: 1 / -1;
      flex-direction: column;
      align-items: stretch;
    }
    .ov-status__actions form {
      width: 100%;
    }
    .ov-status__note {
      text-align: left;
      max-width: none;
    }
  }
</style>
