<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import {
    AlertBanner,
    Button,
    ButtonLink,
    Card,
    Chip,
    EmptyState,
    PageHead,
    PageToolbar,
    MasterToggle,
    StatTile,
    getI18n,
    guildBotState
  } from '@bagel/kit';
  import { DISCORD_SLUG_KEYS } from '$lib/discord-messages';
  import DiscordStateTag from '$lib/components/discord/DiscordStateTag.svelte';
  import GuildCrest from '$lib/components/discord/GuildCrest.svelte';
  import { sinceParts } from '$lib/discord/guild-view';
  import type { DiscordGuildSummary } from '$lib/server/discord-store';

  let { data } = $props();
  const { t } = getI18n();

  // svelte-ignore state_referenced_locally
  let enabled = $state<boolean>(data.enabled ?? false);
  // svelte-ignore state_referenced_locally
  let seed = data;
  $effect(() => {
    if (data !== seed) {
      seed = data;
      enabled = data.enabled ?? false;
    }
  });

  const guilds = $derived<DiscordGuildSummary[]>(data.guilds ?? []);

  const online = $derived(guilds.filter((g) => g.botPresent && !g.needsReauth).length);
  const reach = $derived(guilds.reduce((n, g) => n + Math.max(0, g.memberCount), 0));

  function memberLabel(g: DiscordGuildSummary): string {
    if (g.memberCount <= 0) return t('discord.statusNoMembers');
    return t('discord.statusMembers', { n: g.memberCount.toLocaleString() });
  }

  const SINCE_KEYS = {
    minutes: 'discord.sinceMinutes',
    hours: 'discord.sinceHours',
    days: 'discord.sinceDays'
  } as const;

  let now = $state(0);
  $effect(() => {
    now = Date.now();
  });

  function boundLabel(g: DiscordGuildSummary): string {
    const parts = sinceParts(now, g.boundAtMs);
    return parts ? t('discord.hub.linkedFor', { since: t(SINCE_KEYS[parts.unit], { n: String(parts.n) }) }) : '';
  }
</script>

<section class="screen active">
  <PageHead eyebrow={t('discord.hub.eyebrow')} description={t('discord.description')}>
    {t('discord.hub.titlePre')} <em>{t('discord.hub.titleEm')}</em>
  </PageHead>

  {#if data.locked}
    <section class="block reveal" style="--i:0" aria-labelledby="dc-locked-h">
      <h2 id="dc-locked-h" class="block-title">{t('modules.betaLocked')}</h2>
      <Card>
        <p class="lead"><Chip on>{t('modules.betaChip')}</Chip></p>
        <p class="hint">{t('modules.betaLockedBody')}</p>
        <div class="row">
          <ButtonLink variant="primary" href="/billing">{t('modules.betaUpgrade')}</ButtonLink>
        </div>
      </Card>
    </section>
  {/if}

  {#if !data.locked}
    {#if data.degraded}
      <AlertBanner>{t('discord.degraded')}</AlertBanner>
    {/if}

    {#if data.errorSlug && DISCORD_SLUG_KEYS[data.errorSlug]}
      <AlertBanner variant="warn">{t(DISCORD_SLUG_KEYS[data.errorSlug])}</AlertBanner>
    {/if}

    <PageToolbar>
      {#snippet lead()}
        <MasterToggle
          action="?/toggle"
          bind:enabled
          label={t('discord.masterLabel')}
          hint={enabled ? t('discord.masterHintOn') : t('discord.masterHintOff')}
          ariaLabel={t('discord.masterAria')}
          failMessage={t('discord.masterFail')}
        />
      {/snippet}
      {#snippet trail()}
        {#if data.templateURL}
          <ButtonLink variant="ghost" href={data.templateURL} target="_blank" rel="noopener noreferrer">
            {t('discord.createCta')}
          </ButtonLink>
        {/if}
        {#if data.configured}
          <ButtonLink variant="primary" href="/discord/connect" data-sveltekit-reload>
            {t('discord.addCta')}
          </ButtonLink>
        {:else}
          <Button variant="primary" type="button" disabled>{t('discord.addCta')}</Button>
        {/if}
      {/snippet}
    </PageToolbar>

    {#if guilds.length > 0}
      <section class="block reveal" style="--i:0" aria-labelledby="dc-stats-h">
        <h2 id="dc-stats-h" class="bb-sr-only">{t('discord.hub.statsTitle')}</h2>
        <div class="bb-stat-grid bb-stat-grid--auto">
          <StatTile
            label={t('discord.hub.statServers')}
            value={guilds.length.toLocaleString()}
            delta={t('discord.hub.statServersNote')}
            flat
          />
          <StatTile
            label={t('discord.hub.statOnline')}
            value={online.toLocaleString()}
            delta={t('discord.hub.statOnlineNote')}
            flat
          />
          <StatTile
            label={t('discord.hub.statMembers')}
            value={reach.toLocaleString()}
            delta={t('discord.hub.statMembersNote')}
            flat
          />
        </div>
      </section>
    {/if}

    <section class="block reveal" style="--i:1" aria-labelledby="dc-servers-h">
      <h2 id="dc-servers-h" class="block-title">{t('discord.serversTitle')}</h2>

      {#if guilds.length === 0}
        <Card>
          <EmptyState title={t('discord.emptyTitle')} body={t('discord.emptyBody')}>
            {#if data.configured}
              <ButtonLink variant="primary" href="/discord/connect" data-sveltekit-reload>
                {t('discord.addCta')}
              </ButtonLink>
            {:else}
              <p class="hint">{t('discord.connectUnconfigured')}</p>
            {/if}
          </EmptyState>
        </Card>
      {:else}
        <p class="hint">{t('discord.serversHelp')}</p>
        {#if data.truncated}
          <AlertBanner variant="warn">
            {t('discord.serversTruncated', { n: guilds.length.toLocaleString() })}
          </AlertBanner>
        {/if}

        <ul class="servers">
          {#each guilds as g (g.guildId)}
            {@const state = guildBotState(g)}
            <li>
              <Card as="a" href="/discord/{g.guildId}" hover class="server-card">
                <span class="head">
                  <GuildCrest name={g.name || t('discord.unknownServer')} iconUrl={g.iconUrl} />
                  <DiscordStateTag {state} />
                </span>
                <span class="server-name">{g.name || t('discord.unknownServer')}</span>
                <span class="tr-help">{memberLabel(g)}</span>
                {#if boundLabel(g)}<span class="tr-help">{boundLabel(g)}</span>{/if}
              </Card>
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  {/if}
</section>

<style>
  .screen { display: flex; flex-direction: column; gap: 18px; }
  .block { margin: 0; }
  .block-title {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 16px;
    letter-spacing: -0.01em;
    color: var(--bb-white);
    margin: 0 0 12px;
  }
  .hint {
    margin: 0 0 14px;
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    line-height: 1.55;
    color: var(--bb-muted);
  }
  .lead { margin: 0 0 12px; }
  .row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }

  .servers {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 260px), 1fr));
    gap: 14px;
  }
  .servers :global(.server-card) {
    display: flex;
    flex-direction: column;
    gap: 4px;
    text-decoration: none;
    color: inherit;
    height: 100%;
    box-sizing: border-box;
  }
  .head { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-bottom: 10px; }
  .server-name {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 15px;
    color: var(--bb-white);
    overflow-wrap: anywhere;
  }
  .tr-help { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); line-height: 1.45; }
</style>
