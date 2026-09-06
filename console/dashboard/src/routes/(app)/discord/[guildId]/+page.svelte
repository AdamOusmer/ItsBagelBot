<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The guild overview: what is running in this server, and what is stopping
  // the rest from running.
  //
  // It replaces the old page's status card as the landing surface. The tiles
  // are derived, not authored: guildModuleTiles reads each flag at its real
  // default and reports whether the channel or role it needs was ever picked,
  // which is the difference between "off" and "on but silently dropping every
  // post" -- a distinction the old page never made anywhere.
  import { AlertBanner, ButtonLink, Card, Icon, getI18n, guildModuleTiles, tilesNeedingSetup, type IconName, type ModuleTileId } from '@bagel/shared';
  import ModuleTile from '$lib/components/discord/ModuleTile.svelte';
  import { CLOSE_KEYS, type I18nKey } from '$lib/discord/guild-fields';
  import { botOnlineOf, layoutDownOf } from '$lib/discord/guild-view';
  import { DISCORD_PILL_KEYS } from '$lib/discord-messages';
  import { pillStateOf } from '$lib/discord/guild-view';

  let { data } = $props();
  const { t } = getI18n();

  const tiles = $derived(guildModuleTiles(data.config));
  const unfinished = $derived(tilesNeedingSetup(tiles));
  const botOnline = $derived(botOnlineOf(data));
  const pillState = $derived(pillStateOf(data));
  const layoutDown = $derived(layoutDownOf(data.layout));
  const closeKey = $derived(CLOSE_KEYS[data.status?.lastCloseCode ?? 0]);

  const PILL_ICONS = { online: 'check', offline: 'ban', reauth: 'power', unknown: 'dots' } as const;

  // Typed literal maps rather than a built key: the i18n generator only sees
  // literals, so a tile added without copy fails the type check instead of
  // rendering its own key at a streamer.
  const TILE_ICONS: Record<ModuleTileId, IconName> = {
    announcementsLive: 'broadcast',
    announcementsClips: 'megaphone',
    welcome: 'smile',
    goodbye: 'follower',
    voiceHub: 'mic',
    logs: 'audit',
    levels: 'tally',
    linkGuard: 'link',
    subscribers: 'gem',
    autoRole: 'users',
    tickets: 'ticket'
  };

  const TILE_NAME_KEYS: Record<ModuleTileId, I18nKey> = {
    announcementsLive: 'discord.overview.tiles.announcementsLive.name',
    announcementsClips: 'discord.overview.tiles.announcementsClips.name',
    welcome: 'discord.overview.tiles.welcome.name',
    goodbye: 'discord.overview.tiles.goodbye.name',
    voiceHub: 'discord.overview.tiles.voiceHub.name',
    logs: 'discord.overview.tiles.logs.name',
    levels: 'discord.overview.tiles.levels.name',
    linkGuard: 'discord.overview.tiles.linkGuard.name',
    subscribers: 'discord.overview.tiles.subscribers.name',
    autoRole: 'discord.overview.tiles.autoRole.name',
    tickets: 'discord.overview.tiles.tickets.name'
  };

  const TILE_HELP_KEYS: Record<ModuleTileId, I18nKey> = {
    announcementsLive: 'discord.overview.tiles.announcementsLive.help',
    announcementsClips: 'discord.overview.tiles.announcementsClips.help',
    welcome: 'discord.overview.tiles.welcome.help',
    goodbye: 'discord.overview.tiles.goodbye.help',
    voiceHub: 'discord.overview.tiles.voiceHub.help',
    logs: 'discord.overview.tiles.logs.help',
    levels: 'discord.overview.tiles.levels.help',
    linkGuard: 'discord.overview.tiles.linkGuard.help',
    subscribers: 'discord.overview.tiles.subscribers.help',
    autoRole: 'discord.overview.tiles.autoRole.help',
    tickets: 'discord.overview.tiles.tickets.help'
  };
</script>

<!-- Bound but never saved: nothing in this server has been set up yet, so the
     one useful action is the fill, not a settings page. -->
{#if !data.found}
  <AlertBanner variant="warn" icon="server">
    {t('discord.overview.notSetUp')}
    {#snippet action()}
      <ButtonLink variant="secondary" icon="server" href="/discord/{data.guildId}/settings">
        {t('discord.setupCta')}
      </ButtonLink>
    {/snippet}
  </AlertBanner>
{/if}

<section class="block reveal" style="--i:1" aria-labelledby="dc-status-h">
  <h2 id="dc-status-h" class="block-title">{t('discord.statusTitle')}</h2>
  <Card>
    <dl class="facts">
      <div class="fact">
        <dt>{t('discord.overview.botState')}</dt>
        <dd>
          <span class="pill {pillState}">
            <Icon name={PILL_ICONS[pillState]} size={13} />
            {t(DISCORD_PILL_KEYS[pillState])}
          </span>
        </dd>
      </div>
      <div class="fact">
        <dt>{t('discord.statusResumes')}</dt>
        <dd>{data.status?.sessionResumes ?? 0}</dd>
      </div>
      <div class="fact">
        <dt>{t('discord.overview.modulesOn')}</dt>
        <dd>{tiles.filter((tile) => tile.on).length} / {tiles.length}</dd>
      </div>
    </dl>

    {#if !botOnline && closeKey}
      <p class="hint state">{t(closeKey)}</p>
    {:else if !botOnline}
      <p class="hint state">{t('discord.statusReconnecting')}</p>
    {/if}

    <!-- The pickers on Channels and Roles are what a layout outage disables, so
         it is worth saying here too: an overview that looks fine and a Channels
         page full of dead dropdowns reads as a broken dashboard. -->
    {#if layoutDown}
      <p class="hint state">{t('discord.layoutUnavailable')}</p>
    {/if}
  </Card>
</section>

<section class="block reveal" style="--i:2" aria-labelledby="dc-modules-h">
  <h2 id="dc-modules-h" class="block-title">{t('discord.overview.modulesTitle')}</h2>
  <p class="hint">
    {#if unfinished.length > 0}
      {t('discord.overview.needsSetupHint', { n: String(unfinished.length) })}
    {:else}
      {t('discord.overview.modulesHelp')}
    {/if}
  </p>
  <div class="tiles">
    {#each tiles as tile (tile.id)}
      <ModuleTile
        {tile}
        guildId={data.guildId}
        version={data.version}
        icon={TILE_ICONS[tile.id]}
        name={t(TILE_NAME_KEYS[tile.id])}
        help={t(TILE_HELP_KEYS[tile.id])}
      />
    {/each}
  </div>
</section>

<style>
  .facts {
    display: flex;
    flex-wrap: wrap;
    gap: 26px;
    margin: 0;
  }
  .fact {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .facts dt {
    font-family: var(--bb-font-body);
    font-size: 11px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .facts dd {
    margin: 0;
    font-family: var(--bb-font-mono);
    font-size: 13px;
    color: var(--bb-white);
    font-variant-numeric: tabular-nums;
  }
  .state {
    margin: 16px 0 0;
  }

  .pill {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 4px 11px;
    border-radius: var(--bb-radius-pill);
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    border: 1px solid var(--glass-border);
    white-space: nowrap;
  }
  /* Never colour alone: each pill carries its own icon and its own word. */
  .pill.online {
    color: var(--bb-green-glow);
    background: rgba(82, 183, 136, 0.12);
  }
  .pill.offline {
    color: #cf8a78;
    background: rgba(176, 90, 70, 0.12);
  }
  .pill.reauth {
    color: var(--bb-tan-light);
    background: rgba(201, 168, 124, 0.14);
  }
  .pill.unknown {
    color: var(--bb-muted);
    background: rgba(136, 128, 119, 0.14);
  }

  .tiles {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 280px), 1fr));
    gap: 14px;
  }
</style>
