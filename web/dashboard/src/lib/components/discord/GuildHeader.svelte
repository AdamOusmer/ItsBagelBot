<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The guild shell's identity strip: which server you are in, whether its bot
  // is up, and how to get to another one without going back to the list.
  //
  // It lives in the layout rather than on each page because it is the answer to
  // "where am I", and a page that renders it itself is a page that can render a
  // different answer.
  import { goto } from '$app/navigation';
  import {
    ButtonLink,
    SegmentedControl,
    getI18n,
    guildMonogram,
    type GuildBotState
  } from '@bagel/kit';
  import { DISCORD_PILL_KEYS } from '$lib/discord-messages';
  import { sinceParts } from '$lib/discord/guild-view';
  import type { DiscordGuildSummary } from '$lib/server/discord-store';

  let {
    guildId,
    guilds,
    name,
    memberCount,
    pillState,
    sinceMs
  }: {
    guildId: string;
    guilds: DiscordGuildSummary[];
    name: string;
    memberCount: number;
    pillState: GuildBotState;
    sinceMs: number;
  } = $props();

  const { t } = getI18n();

  const guildName = $derived(name || t('discord.unknownServer'));
  // Discord serves guild icons from its own CDN, and the console CSP is
  // img-src 'self' data:, so an <img> pointed at cdn.discordapp.com renders as
  // a broken box with a console error and no way to fix it short of proxying
  // every guild icon through the dashboard. A monogram tile costs nothing,
  // never 404s and cannot leak the visit to Discord.
  const monogram = $derived(guildMonogram(guildName));
  const members = $derived(memberCount > 0 ? memberCount.toLocaleString() : '');

  // now stays 0 until the browser sets it, so the server and the first client
  // render agree: an uptime rendered during SSR is stale by the time it lands
  // and hydration screams about the mismatch.
  let now = $state(0);
  $effect(() => {
    now = Date.now();
  });

  const SINCE_KEYS = {
    minutes: 'discord.sinceMinutes',
    hours: 'discord.sinceHours',
    days: 'discord.sinceDays'
  } as const;

  const uptime = $derived.by(() => {
    const parts = sinceParts(now, sinceMs);
    return parts ? t(SINCE_KEYS[parts.unit], { n: String(parts.n) }) : '';
  });

  /**
   * Switcher labels, made unique.
   *
   * Discord happily lets one person own two servers with the same name, and
   * SegmentedControl keys its options by their string. Without the suffix the
   * second one would be unclickable and the first would look selected for both.
   */
  function uniqueLabels(names: string[]): string[] {
    const seen = new Map<string, number>();
    return names.map((raw) => {
      const label = raw || t('discord.unknownServer');
      const n = (seen.get(label) ?? 0) + 1;
      seen.set(label, n);
      return n === 1 ? label : `${label} (${n})`;
    });
  }

  const switchLabels = $derived(uniqueLabels(guilds.map((g) => g.name)));
  const switchIndex = $derived(guilds.findIndex((g) => g.guildId === guildId));
  const switchValue = $derived(switchLabels[switchIndex] ?? switchLabels[0] ?? '');
  // Segmented up to three, a select past that: four pills already wrap the
  // strip on a laptop, and the hub is the right surface for browsing more than
  // a handful.
  const SEGMENTED_MAX = 3;

  function switchTo(next: string) {
    if (!next || next === guildId) return;
    goto(`/discord/${next}`);
  }

  function switchToLabel(label: string) {
    const i = switchLabels.indexOf(label);
    if (i < 0) return;
    switchTo(guilds[i].guildId);
  }
</script>

<div class="guild-head">
  <span class="crest" aria-hidden="true">{monogram}</span>

  <div class="copy">
    <h1 class="name">{guildName}</h1>
    <div class="facts">
      <span class="pill {pillState}">
        {t(DISCORD_PILL_KEYS[pillState])}
      </span>
      <span class="tr-help">
        {#if members}{t('discord.statusMembers', { n: members })}{:else}{t('discord.statusNoMembers')}{/if}
      </span>
      {#if uptime}
        <span class="tr-help">{t('discord.statusSince')} {uptime}</span>
      {/if}
    </div>
  </div>

  <div class="switcher">
    <ButtonLink variant="ghost" href="/discord">{t('discord.allServersCta')}</ButtonLink>
    {#if guilds.length > 1 && guilds.length <= SEGMENTED_MAX}
      <SegmentedControl
        options={switchLabels}
        label={t('discord.switcherLabel')}
        bind:value={() => switchValue, switchToLabel}
      />
    {:else if guilds.length > SEGMENTED_MAX}
      <label class="sr-only" for="dc-switcher">{t('discord.switcherLabel')}</label>
      <select id="dc-switcher" class="setting-input" value={guildId} onchange={(e) => switchTo(e.currentTarget.value)}>
        {#each guilds as g, i (g.guildId)}
          <option value={g.guildId}>{switchLabels[i]}</option>
        {/each}
      </select>
    {/if}
  </div>
</div>

<style>
  .guild-head {
    display: flex;
    align-items: center;
    gap: 14px;
    flex-wrap: wrap;
    min-width: 0;
  }
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
  .copy {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
    flex: 1;
  }
  /* The page's one h1: the guild IS the page here, so the shell owns the
     heading and no sub-page renders a second one. */
  .name {
    margin: 0;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 19px;
    letter-spacing: -0.01em;
    color: var(--bb-white);
    overflow-wrap: anywhere;
  }
  .facts {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .tr-help {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
    line-height: 1.45;
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
  /* Steel, deliberately neither green nor red: this guild's reauth flag was
     never read, so a coloured pill would assert health or a fault nobody
     checked. */
  .pill.unknown {
    color: var(--bb-muted);
    background: rgba(136, 128, 119, 0.14);
  }

  .switcher {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    min-width: 0;
  }
  .switcher select {
    width: min(220px, 60vw);
    padding: 8px 12px;
    border: 1px solid var(--rule);
    border-radius: 6px;
    background: rgba(240, 236, 228, 0.04);
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 13px;
    appearance: auto;
  }
  .switcher select option {
    color: #1a1814;
  }
</style>
