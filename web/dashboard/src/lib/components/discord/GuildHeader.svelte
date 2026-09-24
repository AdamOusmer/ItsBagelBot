<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { goto } from '$app/navigation';
  import {
    ButtonLink,
    SegmentedControl,
    Select,
    getI18n,
    type GuildBotState
  } from '@bagel/kit';
  import DiscordStateTag from './DiscordStateTag.svelte';
  import GuildCrest from './GuildCrest.svelte';
  import { sinceParts } from '$lib/discord/guild-view';
  import type { DiscordGuildSummary } from '$lib/server/discord-store';

  let {
    guildId,
    guilds,
    name,
    memberCount,
    iconUrl = '',
    pillState,
    sinceMs
  }: {
    guildId: string;
    guilds: DiscordGuildSummary[];
    name: string;
    iconUrl?: string;
    memberCount: number;
    pillState: GuildBotState;
    sinceMs: number;
  } = $props();

  const { t } = getI18n();

  const guildName = $derived(name || t('discord.unknownServer'));
  const members = $derived(memberCount > 0 ? memberCount.toLocaleString() : '');

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
  <GuildCrest name={guildName} {iconUrl} />

  <div class="copy">
    <h1 class="name">{guildName}</h1>
    <div class="facts">
      <DiscordStateTag state={pillState} />
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
      <label class="bb-sr-only" for="dc-switcher">{t('discord.switcherLabel')}</label>
      <Select id="dc-switcher" class="guild-select" value={guildId} onchange={(e: Event) => switchTo((e.currentTarget as HTMLSelectElement).value)}>
        {#each guilds as g, i (g.guildId)}
          <option value={g.guildId}>{switchLabels[i]}</option>
        {/each}
      </Select>
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
  .copy {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
    flex: 1;
  }
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

  .switcher {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    min-width: 0;
  }
  .switcher { --input-w: min(220px, 60vw); }
</style>
