<script lang="ts">
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
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Button, ButtonLink, Icon, Skeleton, getI18n, type ConnUi } from '@bagel/shared';
  import { statusTone } from './status';

  const { t } = getI18n();

  let {
    loading = false,
    ui,
    checkingText,
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

<section class="ov-status card sheen" class:ov-status--premium={isPremium} aria-labelledby="ov-status-h">
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
          onclick={() => onRestart?.()}
        >{kind === 'degraded' ? t('common.reconnect') : t('overview.restart')}</Button>
        <Button variant="ghost" icon="power" type="button" class="ov-cta" onclick={() => onDisconnect?.()}>{t('overview.disconnect')}</Button>
      {:else if ui.showEnable}
        <form method="POST" action="?/enable" use:enhance={enableSubmit}>
          <Button variant="primary" icon="power" type="submit" class="ov-cta">{t('overview.enable')}</Button>
        </form>
      {:else if ui.showConnect}
        <ButtonLink href="/settings" variant="primary" icon="power" class="ov-cta">{t('overview.issueNoAuthCta')}</ButtonLink>
      {:else if ui.canRetry}
        <ButtonLink href="/" variant="ghost" icon="activity" class="ov-cta">{t('overview.retry')}</ButtonLink>
      {/if}
    </div>
  {/if}
</section>

<style>
  .ov-status {
    display: grid;
    grid-template-columns: auto 1fr auto;
    gap: 22px;
    align-items: center;
    margin-bottom: var(--row-gap);
  }
  .ov-status--premium {
    border-color: rgba(201, 168, 124, 0.4);
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
  .ov-status--premium .ov-status__mark {
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
    border-radius: var(--bb-radius-pill);
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
    .ov-status {
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
