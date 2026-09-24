<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { AlertBanner, ButtonLink, Card, getI18n, guildModuleTiles, tilesNeedingSetup, type ModuleTileId } from '@bagel/kit';
  import ModuleTile from '$lib/components/discord/ModuleTile.svelte';
  import { CLOSE_KEYS, type I18nKey } from '$lib/discord/guild-fields';
  import { botOnlineOf, layoutDownOf, pillStateOf } from '$lib/discord/guild-view';
  import DiscordStateTag from '$lib/components/discord/DiscordStateTag.svelte';

  let { data } = $props();
  const { t } = getI18n();

  const tiles = $derived(guildModuleTiles(data.config));
  const unfinished = $derived(tilesNeedingSetup(tiles));
  const botOnline = $derived(botOnlineOf(data));
  const pillState = $derived(pillStateOf(data));
  const layoutDown = $derived(layoutDownOf(data.layout));
  const closeKey = $derived(CLOSE_KEYS[data.status?.lastCloseCode ?? 0]);

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

{#if !data.found}
  <AlertBanner variant="warn">
    {t('discord.overview.notSetUp')}
    {#snippet action()}
      <ButtonLink variant="secondary" href="/discord/{data.guildId}/settings">
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
          <DiscordStateTag state={pillState} />
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

  .tiles {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 280px), 1fr));
    gap: 14px;
  }
</style>
