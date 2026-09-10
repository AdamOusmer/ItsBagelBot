<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The bot account's own OAuth state, plus the link that starts its consent
  // flow. Owner-only (the caller gates on allows(role, 'bot.token')): this is
  // the one flow that mints a live Twitch credential from a URL that looks
  // unauthenticated.
  import { onMount } from 'svelte';
  import Card from '@bagel/kit/components/Card.svelte';
  import CardHead from '@bagel/kit/components/CardHead.svelte';
  import Button from '@bagel/kit/components/Button.svelte';
  import { statusTone } from '@bagel/kit/status-tone';
  import { copyFlash } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import StatusDot from '../StatusDot.svelte';

  let { present }: { present: boolean } = $props();

  const { t } = getI18n();

  // Absolute URL of the bot-authorization route. The operator opens it in the
  // browser signed into the bot account; that browser gets the state cookie and
  // the callback validates it there, so the link works across the browser switch.
  let botLink = $state('');
  let copied = $state(false);
  onMount(() => {
    botLink = `${location.origin}/auth/bot/login`;
  });

  const copy = () => copyFlash(botLink, (on) => (copied = on));
</script>

<Card as="section">
  <CardHead title={t('admin.overview.botTitle')} />

  <div class="row">
    <div class="mark"><img src="/logo.png" alt="" /></div>
    <div class="who">
      <div class="live">
        <StatusDot tone={statusTone(present ? 'online' : 'auth_required')} />
        {present ? t('admin.overview.botStored') : t('admin.overview.botMissing')}
      </div>
      <div class="meta">
        {present ? t('admin.overview.botStoredMeta') : t('admin.overview.botMissingMeta')}
      </div>
    </div>
    <a class="btn ghost" href="/auth/bot/login">
      {present ? t('admin.overview.botReauthorize') : t('admin.overview.botAuthorize')}
    </a>
  </div>

  {#if botLink}
    <div class="link">
      <p class="hint">{t('admin.overview.botHint')}</p>
      <div class="link-row">
        <input
          class="link-url"
          type="text"
          readonly
          value={botLink}
          aria-label={t('admin.overview.botLinkLabel')}
        />
        <Button variant="ghost" type="button" onclick={copy}>
          {copied ? t('common.copied') : t('common.copy')}
        </Button>
      </div>
    </div>
  {/if}
</Card>

<style>
  .row {
    display: flex;
    align-items: center;
    gap: 14px;
  }
  .mark {
    width: 44px;
    height: 44px;
    border-radius: 50%;
    flex: none;
    background: rgba(82, 183, 136, 0.07);
    border: 1px solid rgba(82, 183, 136, 0.3);
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .mark img {
    width: 30px;
    height: 30px;
    border-radius: 50%;
  }
  .live {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--bb-white);
    margin-bottom: 4px;
  }
  .meta {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
  }
  .row .btn {
    margin-left: auto;
    white-space: nowrap;
  }

  .link {
    margin-top: 14px;
  }
  .hint {
    margin: 0 0 6px;
    font-size: 0.8rem;
    color: var(--bb-muted);
    font-family: var(--bb-font-body);
  }
  .link-row {
    display: flex;
    gap: 8px;
    align-items: center;
  }
  .link-url {
    flex: 1;
    min-width: 0;
    padding: 7px 10px;
    font-family: var(--bb-font-mono);
    font-size: 12px;
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-sm);
    background: var(--bb-bg-1, #16130f);
    color: var(--bb-white);
  }

  @media (max-width: 760px) {
    .row {
      flex-wrap: wrap;
    }
    .row .btn {
      margin-left: 0;
      width: 100%;
      justify-content: center;
    }
  }
</style>
