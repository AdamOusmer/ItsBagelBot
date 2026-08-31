<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The bot status panel: the page's anchor. It tells the streamer, in words,
  // exactly what state the connection is in and offers the recovery action that
  // state needs. Colour + dot are decoration on top of the text label, never the
  // only signal.
  //
  // The state itself comes straight from MAIN's honest connection model: the
  // resolved `ConnUi` (kind + the canManage/showEnable/showConnect/canRetry
  // booleans) decides both the words and which action renders, so a down /
  // pending / failing connection can never masquerade as online. A delegate sees
  // the state read-only, because the enable/restart/disconnect actions all 403
  // for a delegate session server-side.
  //
  // Online is the common case, so it gets the quiet treatment: one row, dot +
  // title + meta + the two management actions as small pills. Every other kind
  // (still loading, or genuinely needs attention) keeps the larger card — icon
  // tile, title, a sentence of detail, and one primary recovery action — because
  // those are the moments the streamer actually has to read and act on.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import Button from '@bagel/shared/components/Button.svelte';
  import ButtonLink from '@bagel/shared/components/ButtonLink.svelte';
  import Card from '@bagel/shared/components/Card.svelte';
  import Icon from '@bagel/shared/components/Icon.svelte';
  import Skeleton from '@bagel/shared/components/Skeleton.svelte';
  import { getI18n } from '@bagel/shared/i18n/context';
  import type { ConnUi } from '@bagel/shared/connection-state';
  import { statusTone } from './status';

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

  // The one-line strip only ever replaces the ONLINE reading, and only once we
  // actually know that (not mid-check) — a loading online guess must not skip
  // straight to the quiet row.
  const strip = $derived(!loading && kind === 'online');

  // The connection state, in words. This IS the "last known connection state",
  // one label per ConnKind, reusing main's existing status vocabulary.
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
      case 'sub_unknown':
        return t('overview.connectedIdle');
      case 'unavailable':
        return t('overview.unavailable');
      default:
        // disabled | auth_required
        return t('overview.notConnected');
    }
  });

  // A one-line reason under the state, again reusing main's copy where it exists.
  const detail = $derived.by(() => {
    switch (kind) {
      case 'online':
        return t('overview.allGood');
      case 'degraded':
        return t('overview.issueSubs');
      case 'reauth_required':
        return t('overview.issueReauth');
      case 'sub_unknown':
        return t('overview.issueIdle');
      case 'disabled':
        return t('overview.statusPausedDetail');
      case 'auth_required':
        return t('overview.issueNoAuth');
      case 'unavailable':
        return t('overview.commandsUnavailableDesc');
      default:
        // connecting: the title already says "Connecting…"; no extra line.
        return '';
    }
  });
</script>

{#if strip}
  <!-- Online: dot, title, plan meta, then the two management pills — one row,
       wraps under itself on narrow viewports instead of forcing scroll. -->
  <Card as="section" sheen class="ov-status ov-status--strip" aria-label={t('overview.statusHeading')}>
    <span class="ov-strip__dot" class:live aria-hidden="true"></span>
    <span class="ov-strip__title">{title}</span>
    {#if planLabel}<span class="ov-strip__meta">{planLabel}</span>{/if}
    <div class="ov-strip__spacer"></div>
    {#if isDelegate}
      <p class="ov-status__note">{t('overview.statusDelegateDetail')}</p>
    {:else if ui?.canManage}
      <button type="button" class="ov-pillbtn" disabled={busy} onclick={() => onRestart?.()}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" />
        </svg>
        {t('overview.restart')}
      </button>
      <button type="button" class="ov-pillbtn" disabled={busy} onclick={() => onDisconnect?.()}>
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
          <span class="sr-only">{checkingText}</span>
          <span aria-hidden="true"><Skeleton variant="text" width="14ch" /></span>
        </p>
        <p class="ov-status__detail" aria-hidden="true"><Skeleton variant="text" lines={2} width="90%" /></p>
      {:else}
        <p class="ov-status__state tone-{tone}">
          <span class="dot" class:live aria-hidden="true"></span>
          <span class="state-text">{title}</span>
        </p>
        {#if detail}<p class="ov-status__detail">{detail}</p>{/if}

        {#if planLabel}
          <div class="ov-status__meta">
            {#if isPremium}
              <span class="tag tag--premium"><Icon name="gem" size={12} /> {planLabel}</span>
            {:else}
              <span class="tag">{planLabel}</span>
            {/if}
          </div>
        {/if}
      {/if}
    </div>

    {#if !loading && ui}
      <div class="ov-status__actions">
        {#if isDelegate}
          <p class="ov-status__note">{t('overview.statusDelegateDetail')}</p>
        {:else if ui.canManage}
          <!-- Active channel: main's restart + disconnect. A degraded connection
               promotes reconnect to the primary action; a healthy one stays quiet. -->
          <Button
            variant={kind === 'degraded' ? 'primary' : 'ghost'}
            icon="activity"
            type="button"
            class="ov-cta"
            disabled={busy}
            onclick={() => onRestart?.()}
          >{kind === 'degraded' ? t('common.reconnect') : t('overview.restart')}</Button>
          <Button variant="ghost" icon="power" type="button" class="ov-cta" disabled={busy} onclick={() => onDisconnect?.()}>{t('overview.disconnect')}</Button>
        {:else if ui.showEnable}
          <form method="POST" action="?/enable" use:enhance={enableSubmit}>
            <Button variant="primary" icon="power" type="submit" class="ov-cta" loading={busy}>{t('overview.enable')}</Button>
          </form>
        {:else if ui.showConnect}
          <!-- reauth_required: the grant died server-side, only a fresh Twitch
               consent restores it, so the one action offered is the reconnect. -->
          <ButtonLink href="/settings" variant="primary" icon="power" class="ov-cta"
            >{kind === 'reauth_required' ? t('common.reconnect') : t('overview.issueNoAuthCta')}</ButtonLink>
        {:else if ui.canRetry}
          <ButtonLink href="/" variant="ghost" icon="activity" class="ov-cta">{t('overview.retry')}</ButtonLink>
        {/if}
      </div>
    {/if}
  </Card>
{/if}

<style>
  /* :global on every .ov-status / --premium rule: the class now rides <Card>'s
     root element, which this component's scoping hash never reaches. */
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

  /* The one-line strip: dot, bold title, mono meta, spacer, two pill actions.
     flex-wrap is the whole narrow-screen story here — no separate breakpoint
     rules needed, the row just folds under itself. */
  :global(.ov-status--strip) {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 12px;
  }
  .ov-strip__dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    flex: none;
    background: var(--bb-status-success);
  }
  .ov-strip__dot.live {
    box-shadow: 0 0 8px var(--bb-status-success);
    animation: ov-pulse 2.4s ease-in-out infinite;
  }
  @media (prefers-reduced-motion: reduce) {
    .ov-strip__dot.live {
      animation: none;
    }
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
  .ov-pillbtn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-height: 34px;
    padding: 0 14px;
    border-radius: 999px;
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid var(--bb-border);
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    font-weight: 500;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--bb-muted);
    cursor: pointer;
    transition:
      border-color 200ms ease,
      color 200ms ease;
  }
  .ov-pillbtn svg {
    flex: none;
    width: 13px;
    height: 13px;
  }
  .ov-pillbtn:hover:not(:disabled) {
    border-color: var(--bb-tan);
    color: var(--bb-white);
  }
  .ov-pillbtn:disabled {
    opacity: 0.5;
    cursor: default;
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
  /* Tone tints only the dot + a hair of the text; the WORD carries the state. */
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
    width: 10px;
    height: 10px;
    border-radius: 50%;
    flex: none;
    background: var(--bb-muted);
  }
  .tone-success .dot {
    background: var(--bb-status-success);
  }
  .tone-error .dot {
    background: var(--bb-status-error);
  }
  .tone-warning .dot {
    background: var(--bb-status-warning);
  }
  .dot.live {
    box-shadow: 0 0 8px var(--bb-status-success);
    animation: ov-pulse 2.4s ease-in-out infinite;
  }
  @keyframes ov-pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.4; }
  }
  @media (prefers-reduced-motion: reduce) {
    .dot.live { animation: none; }
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
  .tag {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    padding: 5px 12px;
    border-radius: 999px;
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid var(--bb-border);
    color: var(--bb-muted);
  }
  .tag--premium {
    background: rgba(201, 168, 124, 0.12);
    border-color: rgba(201, 168, 124, 0.35);
    color: var(--bb-tan-light);
  }
  .tag--premium :global(svg) {
    stroke: currentColor;
    fill: none;
    stroke-width: 1.7;
  }

  .ov-status__actions {
    display: flex;
    gap: 10px;
    align-items: center;
    flex: none;
  }
  /* Every action clears the 44px target regardless of the shared button's base
     height, and stays >=8px from its neighbour via the row gap above. */
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

  /* Stack the panel on narrow screens; actions become full-width, comfortably
     tappable, and never force horizontal scroll at 320px. */
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
