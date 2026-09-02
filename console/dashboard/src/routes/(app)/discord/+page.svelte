<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Icon,
    Card,
    PageHead,
    MasterToggle,
    AlertBanner,
    Button,
    ButtonLink,
    Field,
    Switch,
    toast,
    getI18n
  } from '@bagel/shared';
  import type { DiscordConfig, DiscordEntry } from '$lib/server/discord-store';

  let { data } = $props();
  const { t } = getI18n();

  function onByDefault(v: string | undefined): boolean {
    return v !== 'off';
  }
  function offByDefault(v: string | undefined): boolean {
    return v === 'on';
  }

  // svelte-ignore state_referenced_locally
  let enabled = $state<boolean>(data.enabled ?? false);
  // svelte-ignore state_referenced_locally
  let config = $state<DiscordConfig>(data.config);
  // svelte-ignore state_referenced_locally
  let seed = data;
  $effect(() => {
    if (data !== seed) {
      seed = data;
      enabled = data.enabled ?? false;
      config = data.config;
    }
  });

  // Discord channel types: 0 text, 2 voice, 4 category.
  const textChannels = $derived((data.layout?.channels ?? []).filter((c: DiscordEntry) => c.type === 0));
  const voiceChannels = $derived((data.layout?.channels ?? []).filter((c: DiscordEntry) => c.type === 2));
  const roles = $derived((data.layout?.roles ?? []).filter((r: DiscordEntry) => r.name !== '@everyone'));
  const hasLayout = $derived(textChannels.length > 0);

  const ERROR_SLUG_KEYS: Record<string, 'discord.errOauth' | 'discord.errUnconfigured' | 'discord.errSetup' | 'discord.errState' | 'discord.errBound'> = {
    oauth: 'discord.errOauth',
    unconfigured: 'discord.errUnconfigured',
    setup: 'discord.errSetup',
    state: 'discord.errState',
    bound: 'discord.errBound'
  };

  type ActionResult = { ok?: boolean; error?: string; refused?: string };
  function payloadOf(result: unknown): ActionResult | undefined {
    const r = result as { type: string; data?: ActionResult };
    return r.type === 'success' || r.type === 'failure' ? r.data : undefined;
  }

  const saveSubmit: SubmitFunction = () => {
    return async ({ result }) => {
      const payload = payloadOf(result);
      if (result.type === 'success' && payload?.ok !== false) {
        toast('ok', t('discord.toastSaved'));
        await invalidateAll();
        return;
      }
      toast('err', payload?.error ?? t('discord.toastSaveFailed'));
    };
  };

  const setupSubmit: SubmitFunction = () => {
    return async ({ result }) => {
      const payload = payloadOf(result);
      if (result.type === 'success' && payload?.ok !== false) {
        if (payload?.refused) toast('err', t('discord.toastRefused'));
        else toast('ok', t('discord.toastSetup'));
        await invalidateAll();
        return;
      }
      toast('err', payload?.error ?? t('discord.toastSetupFailed'));
    };
  };

  const disconnectSubmit: SubmitFunction = () => {
    return async ({ result }) => {
      const payload = payloadOf(result);
      if (result.type === 'success' && payload?.ok !== false) {
        toast('ok', t('discord.toastDisconnected'));
        await invalidateAll();
        return;
      }
      toast('err', payload?.error ?? t('discord.toastDisconnectFailed'));
    };
  };
</script>

{#snippet picker(name: keyof DiscordConfig, label: string, options: DiscordEntry[], prefix: string)}
  <Field {label}>
    {#if hasLayout}
      <select class="input" {name} value={config[name]}>
        <option value="">{t('discord.notSet')}</option>
        {#each options as opt (opt.id)}
          <option value={opt.id}>{prefix}{opt.name}</option>
        {/each}
      </select>
    {:else}
      <input class="input" {name} value={config[name]} placeholder="123456789012345678" inputmode="numeric" />
    {/if}
  </Field>
{/snippet}

<section class="screen active">
  <a class="back" href="/modules"><Icon name="x" size={13} /> {t('discord.back')}</a>
  <PageHead eyebrow={t('discord.eyebrow')} description={t('discord.description')}>
    {t('discord.titlePre')} <em>{t('discord.titleEm')}</em>
  </PageHead>

  {#if data.degraded}
    <AlertBanner>{t('discord.degraded')}</AlertBanner>
  {/if}

  {#if data.errorSlug && ERROR_SLUG_KEYS[data.errorSlug]}
    <AlertBanner variant="warn" icon="ban">{t(ERROR_SLUG_KEYS[data.errorSlug])}</AlertBanner>
  {/if}

  {#if data.justConnected && data.refused}
    <AlertBanner variant="warn" icon="server">{t('discord.connectedLivedIn')}</AlertBanner>
  {/if}

  <div class="toolbar">
    <MasterToggle
      action="?/toggle"
      bind:enabled
      label={t('discord.masterLabel')}
      hint={enabled ? t('discord.masterHintOn') : t('discord.masterHintOff')}
      ariaLabel={t('discord.masterAria')}
      failMessage={t('discord.masterFail')}
    />
  </div>

  {#if !data.connected}
    <Card>
      <div class="step">
        <span class="step-index" aria-hidden="true">1</span>
        <div class="step-body">
          <h2>{t('discord.connectTitle')}</h2>
          <p class="muted-text">{t('discord.connectHelp')}</p>
          <div class="row">
            {#if data.templateURL}
              <ButtonLink variant="secondary" href={data.templateURL} target="_blank" rel="noopener noreferrer">
                {t('discord.createCta')}
              </ButtonLink>
            {/if}
            {#if data.configured}
              <ButtonLink variant="primary" href="/discord/connect" data-sveltekit-reload>
                {t('discord.connectCta')}
              </ButtonLink>
            {:else}
              <Button variant="primary" type="button" disabled>{t('discord.connectCta')}</Button>
            {/if}
          </div>
          {#if !data.configured}
            <p class="muted-text follow">{t('discord.connectUnconfigured')}</p>
          {:else if data.templateURL}
            <p class="muted-text follow">{t('discord.createThenConnect')}</p>
          {/if}
        </div>
      </div>
    </Card>
  {:else}
    <Card>
      <div class="step">
        <span class="step-index" aria-hidden="true">✓</span>
        <div class="step-body">
          <h2>{t('discord.connectedTitle')}</h2>
          <p class="muted-text">{t('discord.connectedGuild')} <strong>{config.guildId}</strong></p>
          <div class="row">
            <form method="POST" action="?/setup" use:enhance={setupSubmit}>
              <Button variant="secondary" type="submit">{t('discord.setupCta')}</Button>
            </form>
            <form method="POST" action="?/disconnect" use:enhance={disconnectSubmit}>
              <Button variant="destructive" type="submit">{t('discord.disconnectCta')}</Button>
            </form>
          </div>
        </div>
      </div>
    </Card>

    <form method="POST" action="?/save" use:enhance={saveSubmit}>
      <Card>
        <h2 class="block-title">{t('discord.pickersTitle')}</h2>
        <p class="muted-text">{t('discord.pickersHelp')}</p>
        {#if !hasLayout}
          <p class="muted-text">{t('discord.layoutUnavailable')}</p>
        {/if}
        <div class="fields">
          {@render picker('liveChannelId', t('discord.liveChannelLabel'), textChannels, '#')}
          {@render picker('clipsChannelId', t('discord.clipsChannelLabel'), textChannels, '#')}
          {@render picker('alertsChannelId', t('discord.alertsChannelLabel'), textChannels, '#')}
          {@render picker('welcomeChannelId', t('discord.welcomeChannelLabel'), textChannels, '#')}
          {@render picker('voiceHubId', t('discord.voiceHubLabel'), voiceChannels, '')}
          {@render picker('liveRoleId', t('discord.liveRoleLabel'), roles, '@')}
        </div>
        <Field label={t('discord.streamerIdLabel')} tag={t('discord.streamerIdTag')}>
          <input class="input wide" name="streamerDiscordId" value={config.streamerDiscordId} placeholder="123456789012345678" inputmode="numeric" />
        </Field>
        <p class="muted-text">{t('discord.streamerIdHelp')}</p>
        <Button variant="primary" type="submit">{t('discord.save')}</Button>
      </Card>

      <Card>
        <h2 class="block-title">{t('discord.postsTitle')}</h2>
        <p class="muted-text">{t('discord.postsHelp')}</p>
        <div class="setting-row">
          <div class="tr-text">
            <span class="tr-label">{t('discord.liveLabel')}</span>
            <span class="tr-help">{t('discord.liveHelp')}</span>
          </div>
          <input type="hidden" name="liveEnabled" value={onByDefault(config.liveEnabled) ? 'on' : 'off'} />
          <Switch
            checked={onByDefault(config.liveEnabled)}
            label={t('discord.liveLabel')}
            onchange={(v) => (config = { ...config, liveEnabled: v ? 'on' : 'off' })}
          />
        </div>
        <div class="setting-row">
          <div class="tr-text">
            <span class="tr-label">{t('discord.clipsLabel')}</span>
            <span class="tr-help">{t('discord.clipsHelp')}</span>
          </div>
          <input type="hidden" name="clipsEnabled" value={onByDefault(config.clipsEnabled) ? 'on' : 'off'} />
          <Switch
            checked={onByDefault(config.clipsEnabled)}
            label={t('discord.clipsLabel')}
            onchange={(v) => (config = { ...config, clipsEnabled: v ? 'on' : 'off' })}
          />
        </div>
        <div class="setting-row">
          <div class="tr-text">
            <span class="tr-label">{t('discord.raidLabel')}</span>
            <span class="tr-help">{t('discord.raidHelp')}</span>
          </div>
          <input type="hidden" name="raidEnabled" value={onByDefault(config.raidEnabled) ? 'on' : 'off'} />
          <Switch
            checked={onByDefault(config.raidEnabled)}
            label={t('discord.raidLabel')}
            onchange={(v) => (config = { ...config, raidEnabled: v ? 'on' : 'off' })}
          />
        </div>
        <div class="setting-row">
          <div class="tr-text">
            <span class="tr-label">{t('discord.giftLabel')}</span>
            <span class="tr-help">{t('discord.giftHelp')}</span>
          </div>
          <input type="hidden" name="giftEnabled" value={onByDefault(config.giftEnabled) ? 'on' : 'off'} />
          <Switch
            checked={onByDefault(config.giftEnabled)}
            label={t('discord.giftLabel')}
            onchange={(v) => (config = { ...config, giftEnabled: v ? 'on' : 'off' })}
          />
        </div>
        <div class="setting-row">
          <div class="tr-text">
            <span class="tr-label">{t('discord.cheerLabel')}</span>
            <span class="tr-help">{t('discord.cheerHelp')}</span>
          </div>
          <input type="hidden" name="cheerEnabled" value={offByDefault(config.cheerEnabled) ? 'on' : 'off'} />
          <Switch
            checked={offByDefault(config.cheerEnabled)}
            label={t('discord.cheerLabel')}
            onchange={(v) => (config = { ...config, cheerEnabled: v ? 'on' : 'off' })}
          />
        </div>
        <div class="setting-row">
          <div class="tr-text">
            <span class="tr-label">{t('discord.milestoneLabel')}</span>
            <span class="tr-help">{t('discord.milestoneHelp')}</span>
          </div>
          <input type="hidden" name="subMilestoneEnabled" value={offByDefault(config.subMilestoneEnabled) ? 'on' : 'off'} />
          <Switch
            checked={offByDefault(config.subMilestoneEnabled)}
            label={t('discord.milestoneLabel')}
            onchange={(v) => (config = { ...config, subMilestoneEnabled: v ? 'on' : 'off' })}
          />
        </div>
        <div class="fields">
          <Field label={t('discord.giftMinLabel')} tag={t('discord.giftMinTag')}>
            <input class="input" type="number" name="giftMin" min="1" step="1" value={config.giftMin || '5'} />
          </Field>
          <Field label={t('discord.cheerMinLabel')} tag={t('discord.cheerMinTag')}>
            <input class="input" type="number" name="cheerMin" min="1" step="1" value={config.cheerMin || '1000'} />
          </Field>
        </div>
        <Field label={t('discord.allowLabel')} tag={t('discord.allowTag')}>
          <input class="input wide" name="categoryAllow" value={config.categoryAllow} placeholder={t('discord.allowPlaceholder')} />
        </Field>
        <Field label={t('discord.denyLabel')} tag={t('discord.denyTag')}>
          <input class="input wide" name="categoryDeny" value={config.categoryDeny} placeholder={t('discord.denyPlaceholder')} />
        </Field>
        <Button variant="primary" type="submit">{t('discord.save')}</Button>
      </Card>

      <Card>
        <h2 class="block-title">{t('discord.communityTitle')}</h2>
        <p class="muted-text">{t('discord.communityHelp')}</p>
        <div class="setting-row">
          <div class="tr-text">
            <span class="tr-label">{t('discord.welcomeLabel')}</span>
            <span class="tr-help">{t('discord.welcomeHelp')}</span>
          </div>
          <input type="hidden" name="welcomeEnabled" value={onByDefault(config.welcomeEnabled) ? 'on' : 'off'} />
          <Switch
            checked={onByDefault(config.welcomeEnabled)}
            label={t('discord.welcomeLabel')}
            onchange={(v) => (config = { ...config, welcomeEnabled: v ? 'on' : 'off' })}
          />
        </div>
        <div class="setting-row">
          <div class="tr-text">
            <span class="tr-label">{t('discord.goodbyeLabel')}</span>
            <span class="tr-help">{t('discord.goodbyeHelp')}</span>
          </div>
          <input type="hidden" name="goodbyeEnabled" value={offByDefault(config.goodbyeEnabled) ? 'on' : 'off'} />
          <Switch
            checked={offByDefault(config.goodbyeEnabled)}
            label={t('discord.goodbyeLabel')}
            onchange={(v) => (config = { ...config, goodbyeEnabled: v ? 'on' : 'off' })}
          />
        </div>
        <div class="setting-row">
          <div class="tr-text">
            <span class="tr-label">{t('discord.voiceLabel')}</span>
            <span class="tr-help">{t('discord.voiceHelp')}</span>
          </div>
          <input type="hidden" name="voiceEnabled" value={onByDefault(config.voiceEnabled) ? 'on' : 'off'} />
          <Switch
            checked={onByDefault(config.voiceEnabled)}
            label={t('discord.voiceLabel')}
            onchange={(v) => (config = { ...config, voiceEnabled: v ? 'on' : 'off' })}
          />
        </div>
        <Button variant="primary" type="submit">{t('discord.save')}</Button>
      </Card>
    </form>
  {/if}
</section>

<style>
  .back {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
    text-decoration: none;
    margin-bottom: 10px;
  }
  .back:hover { color: var(--bb-white); }
  .back:focus-visible { outline: 2px solid var(--bb-focus, var(--bb-tan)); outline-offset: 2px; border-radius: 4px; }

  .toolbar { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; margin-bottom: 18px; }

  .step { display: flex; gap: 14px; align-items: flex-start; }
  .step-index {
    flex: none;
    width: 34px;
    height: 34px;
    border-radius: 8px;
    display: grid;
    place-items: center;
    background: rgba(201, 168, 124, 0.12);
    border: 1px solid var(--glass-border);
    color: var(--bb-tan-light);
    font-family: var(--bb-font-mono, "DM Mono", monospace);
    font-weight: 600;
    font-size: 14px;
  }
  .step-body { flex: 1; min-width: 0; }
  .step-body h2, .block-title {
    margin: 0 0 6px;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 15px;
    color: var(--bb-white);
  }
  .muted-text {
    color: var(--bb-muted);
    font-family: var(--bb-font-body);
    font-size: 13px;
    line-height: 1.55;
    margin: 0 0 14px;
  }
  .muted-text.follow { margin-top: 12px; margin-bottom: 0; }
  .muted-text strong { color: var(--bb-tan-light); font-weight: 600; }

  .row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }

  .setting-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 12px;
    align-items: center;
    padding: 12px 0;
    border-top: 1px solid var(--glass-border);
  }
  .tr-text { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
  .tr-label { font-family: var(--bb-font-body); font-size: 13.5px; color: var(--bb-white); }
  .tr-help { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); line-height: 1.45; }

  .fields { display: grid; grid-template-columns: repeat(auto-fit, minmax(12rem, 1fr)); gap: 12px; margin-top: 8px; }
  .input {
    padding: 8px 12px;
    border-radius: 6px;
    border: 1px solid var(--rule);
    background: rgba(240, 236, 228, 0.04);
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 13px;
    width: 100%;
    box-sizing: border-box;
  }
  select.input { appearance: auto; }
  select.input option { color: #1a1814; }
  .input.wide { min-width: 0; }
  .input:focus { outline: none; border-color: var(--bb-tan, #c9a87c); }
</style>
