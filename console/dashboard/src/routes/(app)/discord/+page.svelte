<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The bot's home: everything true of the ACCOUNT (the master switch, the
  // servers, the invite path). Everything true of one server lives under
  // /discord/[guildId].
  import {
    AlertBanner,
    Button,
    ButtonLink,
    Card,
    Chip,
    EmptyState,
    Icon,
    PageHead,
    PageToolbar,
    MasterToggle,
    StatTile,
    getI18n,
    guildBotState,
    guildMonogram
  } from '@bagel/shared';
  import { DISCORD_PILL_KEYS, DISCORD_SLUG_KEYS } from '$lib/discord-messages';
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

  // Never colour alone: every pill carries its own icon and its own word, and
  // `unknown` gets a neutral one because the listing never read that guild's
  // reauth flag.
  const PILL_ICONS = { online: 'check', offline: 'ban', reauth: 'power', unknown: 'dots' } as const;

  // "Online" here means the gateway is present AND the grant is still good: a
  // guild needing re-authorization is counted as not online, because that is
  // what the streamer has to act on. Counting it as online is how a dead grant
  // hides behind a healthy-looking number.
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

  // now stays 0 until the browser sets it: a "linked 3 d ago" rendered during
  // SSR is stale by the time it lands and hydration reports the mismatch.
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

  <!-- Discord is premium-only while in beta. The route guard lets this page
       load rather than bouncing to /modules, because Discord has no tile
       there any more and a silent redirect explains nothing. The panel is its
       own top-level block instead of wrapping the page in an else-branch, and
       every action refuses server-side regardless of what renders here. -->
  {#if data.locked}
    <section class="block reveal" style="--i:0" aria-labelledby="dc-locked-h">
      <h2 id="dc-locked-h" class="block-title">{t('modules.betaLocked')}</h2>
      <Card>
        <p class="lead"><Chip on>{t('modules.betaChip')}</Chip></p>
        <p class="hint">{t('modules.betaLockedBody')}</p>
        <div class="row">
          <ButtonLink variant="primary" href="/billing" icon="gem">{t('modules.betaUpgrade')}</ButtonLink>
        </div>
      </Card>
    </section>
  {/if}

  {#if !data.locked}
    {#if data.degraded}
      <AlertBanner>{t('discord.degraded')}</AlertBanner>
    {/if}

    {#if data.errorSlug && DISCORD_SLUG_KEYS[data.errorSlug]}
      <AlertBanner variant="warn" icon="ban">{t(DISCORD_SLUG_KEYS[data.errorSlug])}</AlertBanner>
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
          <ButtonLink variant="ghost" icon="plus" href={data.templateURL} target="_blank" rel="noopener noreferrer">
            {t('discord.createCta')}
          </ButtonLink>
        {/if}
        {#if data.configured}
          <ButtonLink variant="primary" icon="discord" href="/discord/connect" data-sveltekit-reload>
            {t('discord.addCta')}
          </ButtonLink>
        {:else}
          <Button variant="primary" type="button" disabled>{t('discord.addCta')}</Button>
        {/if}
      {/snippet}
    </PageToolbar>

    <!-- The strip answers "is my bot working" before the list answers "where".
         It is hidden with no servers, where three zeros say nothing the empty
         state does not say better. -->
    {#if guilds.length > 0}
      <section class="block reveal" style="--i:0" aria-labelledby="dc-stats-h">
        <h2 id="dc-stats-h" class="sr-only">{t('discord.hub.statsTitle')}</h2>
        <div class="stat-grid three">
          <StatTile
            icon="server"
            tan
            label={t('discord.hub.statServers')}
            value={guilds.length.toLocaleString()}
            delta={t('discord.hub.statServersNote')}
            flat
          />
          <StatTile
            icon="broadcast"
            label={t('discord.hub.statOnline')}
            value={online.toLocaleString()}
            delta={t('discord.hub.statOnlineNote')}
            flat
          />
          <StatTile
            icon="users"
            tan
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
          <EmptyState icon="discord" title={t('discord.emptyTitle')} body={t('discord.emptyBody')}>
            {#if data.configured}
              <ButtonLink variant="primary" icon="discord" href="/discord/connect" data-sveltekit-reload>
                {t('discord.addCta')}
              </ButtonLink>
            {:else}
              <p class="hint">{t('discord.connectUnconfigured')}</p>
            {/if}
          </EmptyState>
        </Card>
      {:else}
        <p class="hint">{t('discord.serversHelp')}</p>
        <!-- outgress caps how many bindings it describes. Without saying so, a
             streamer over the cap sees a short list and no sign of it, which
             reads as Bagel having lost a server. -->
        {#if data.truncated}
          <AlertBanner variant="warn" icon="list">
            {t('discord.serversTruncated', { n: guilds.length.toLocaleString() })}
          </AlertBanner>
        {/if}

        <!-- The whole card is the link, not an Open button in its corner: the
             card has one destination, and a 44px button inside a 260px target
             makes the other 90% of it dead space under a thumb. -->
        <ul class="servers">
          {#each guilds as g (g.guildId)}
            {@const state = guildBotState(g)}
            <li>
              <Card as="a" href="/discord/{g.guildId}" hover class="server-card">
                <span class="head">
                  <!-- Monogram, not the guild icon: the console CSP is
                       img-src 'self' data:, so a CDN <img> renders as a broken
                       box and leaks the visit to Discord besides. -->
                  <span class="crest" aria-hidden="true">{guildMonogram(g.name || t('discord.unknownServer'))}</span>
                  <span class="pill {state}">
                    <Icon name={PILL_ICONS[state]} size={13} />
                    {t(DISCORD_PILL_KEYS[state])}
                  </span>
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

  /* Three tiles, not the shared grid's four. auto-fit rather than a media
     query because app.css's own 1100px rule for .stat-grid would otherwise be
     out-specified by this selector and never apply. */
  .stat-grid.three { grid-template-columns: repeat(auto-fit, minmax(min(100%, 200px), 1fr)); }

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
  .crest {
    flex: none;
    width: 44px;
    height: 44px;
    border-radius: 8px;
    display: grid;
    place-items: center;
    background: rgba(201, 168, 124, 0.12);
    border: 1px solid var(--glass-border);
    color: var(--bb-tan-light);
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 15px;
    letter-spacing: 0.02em;
  }
  .server-name {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 15px;
    color: var(--bb-white);
    overflow-wrap: anywhere;
  }
  .tr-help { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); line-height: 1.45; }

  .pill {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 5px 12px;
    border-radius: var(--bb-radius-pill);
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    border: 1px solid var(--glass-border);
    white-space: nowrap;
  }
  /* Never colour alone: each pill carries its own icon and its own word. */
  .pill.online { color: var(--bb-green-glow); background: rgba(82, 183, 136, 0.12); }
  .pill.offline { color: #cf8a78; background: rgba(176, 90, 70, 0.12); }
  .pill.reauth { color: var(--bb-tan-light); background: rgba(201, 168, 124, 0.14); }
  /* Steel, deliberately neither green nor red: this guild's reauth flag was
     never read, so a coloured pill would assert health or a fault that nobody
     checked. */
  .pill.unknown { color: var(--bb-muted); background: rgba(136, 128, 119, 0.14); }
</style>
