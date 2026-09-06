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
    Icon,
    PageHead,
    PageToolbar,
    MasterToggle,
    getI18n,
    guildBotState,
    guildMonogram
  } from '@bagel/shared';
  import { DISCORD_PILL_KEYS, DISCORD_SLUG_KEYS } from '$lib/discord-messages';
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

  function memberLabel(g: DiscordGuildSummary): string {
    if (g.memberCount <= 0) return t('discord.statusNoMembers');
    return t('discord.statusMembers', { n: g.memberCount.toLocaleString() });
  }
</script>

<section class="screen active">
  <PageHead eyebrow={t('discord.eyebrow')} description={t('discord.description')}>
    {t('discord.titlePre')} <em>{t('discord.titleEm')}</em>
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

    <section class="block reveal" style="--i:1" aria-labelledby="dc-servers-h">
      <h2 id="dc-servers-h" class="block-title">{t('discord.serversTitle')}</h2>
      <Card>
        {#if guilds.length === 0}
          <EmptyState icon="discord" title={t('discord.emptyTitle')} body={t('discord.emptyBody')}>
            {#if data.configured}
              <ButtonLink variant="primary" icon="discord" href="/discord/connect" data-sveltekit-reload>
                {t('discord.addCta')}
              </ButtonLink>
            {:else}
              <p class="hint">{t('discord.connectUnconfigured')}</p>
            {/if}
          </EmptyState>
        {:else}
          <p class="hint">{t('discord.serversHelp')}</p>
          <!-- outgress caps how many bindings it describes. Without saying so,
               a streamer over the cap sees a short list and no sign of it,
               which reads as Bagel having lost a server. -->
          {#if data.truncated}
            <AlertBanner variant="warn" icon="list">
              {t('discord.serversTruncated', { n: guilds.length.toLocaleString() })}
            </AlertBanner>
          {/if}
          <ul class="servers">
            {#each guilds as g (g.guildId)}
              {@const state = guildBotState(g)}
              <li class="server">
                <!-- Monogram, not the guild icon: the console CSP is
                     img-src 'self' data:, so a CDN <img> renders as a broken
                     box and leaks the visit to Discord besides. -->
                <span class="crest" aria-hidden="true">{guildMonogram(g.name || t('discord.unknownServer'))}</span>
                <span class="server-copy">
                  <span class="server-name">{g.name || t('discord.unknownServer')}</span>
                  <span class="tr-help">{memberLabel(g)}</span>
                </span>
                <span class="server-actions">
                  <span class="pill {state}">
                    <Icon name={PILL_ICONS[state]} size={13} />
                    {t(DISCORD_PILL_KEYS[state])}
                  </span>
                  <ButtonLink variant="secondary" href="/discord/{g.guildId}">
                    {t('discord.openCta')}
                  </ButtonLink>
                </span>
              </li>
            {/each}
          </ul>
        {/if}
      </Card>
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

  .servers { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; }
  .server {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 14px 0;
    border-top: 1px solid var(--glass-border);
  }
  .server:first-child { border-top: none; padding-top: 0; }
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
  .server-copy { display: flex; flex-direction: column; gap: 2px; min-width: 0; flex: 1; }
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

  .server-actions { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }

  @media (max-width: 600px) {
    /* Crest and name stay on one line, pill and button drop to the next,
       indented to the name rather than the card edge: wrapping the copy
       instead put the crest on the second line next to the pill, which read
       like two separate rows. 58px = the 44px crest plus its 14px gap. */
    .server { flex-wrap: wrap; }
    .server-actions { flex-basis: 100%; padding-left: 58px; }
  }
</style>
