<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The guild shell.
  //
  // Everything that is true of the SERVER rather than of one settings page
  // lives here: the identity strip, the sub-nav, and every banner that a
  // streamer must see no matter which sub-page they landed on. A reauth notice
  // rendered only on the overview is a reauth notice nobody reads, because the
  // page a mod bookmarks is Tickets.
  //
  // The two banners that are NOT here are the conflict and refused-field ones:
  // those belong to a draft, drafts are per sub-page, and a banner about a save
  // that happened on Channels has no meaning while you are looking at Roles.
  import { AlertBanner, ButtonLink, Card, Chip, getI18n, droppedPinNotice } from '@bagel/kit';
  import GuildHeader from '$lib/components/discord/GuildHeader.svelte';
  import GuildNav from '$lib/components/discord/GuildNav.svelte';
  import { SLOT_LABEL_KEYS } from '$lib/discord/guild-fields';
  import { guildInfoOf, needsReauthOf, pillStateOf } from '$lib/discord/guild-view';
  // The shared setting-row recipes, as one stylesheet instead of a copy in each
  // of the six sub-pages (see the file header).
  import '$lib/components/discord/guild-forms.css';

  let { data, children } = $props();
  const { t } = getI18n();

  const guild = $derived(guildInfoOf(data));
  const needsReauth = $derived(needsReauthOf(data));
  const pillState = $derived(pillStateOf(data));

  /**
   * Pins the last setup could not honour.
   *
   * A slot pinned to a role that has since been deleted cannot be adopted, so
   * the fill creates a fresh role and the streamer's pick changes underneath
   * them. The server has already cleared the dead pin off the row; this is the
   * only place that says it happened, and it says it once -- the notice rides a
   * one-shot cookie the load deletes as it reads it.
   */
  const droppedPins = $derived(droppedPinNotice(data.droppedPins ?? []));
  const droppedBanner = $derived(
    droppedPins.length === 0
      ? ''
      : t('discord.droppedPins', { slots: droppedPins.map((slot) => t(SLOT_LABEL_KEYS[slot])).join(', ') })
  );
</script>

<section class="screen active dc">
  <!-- Discord is premium-only while in beta. The route guard lets these pages
       load rather than bouncing to /modules, because Discord has no tile there
       any more and a silent redirect explains nothing. The panel replaces the
       shell entirely, and every action refuses server-side regardless of what
       renders here. -->
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
  {:else}
    {#if data.degraded}
      <AlertBanner>{t('discord.degraded')}</AlertBanner>
    {/if}

    {#if data.justConnected && data.refused}
      <AlertBanner variant="warn">{t('discord.connectedLivedIn')}</AlertBanner>
    {/if}

    <!-- Setup could not adopt a pinned role because it is gone from the guild,
         so it created a replacement. Shown once: the notice arrives on a
         one-shot cookie the load deletes as it reads it, and the dead pin is
         already off the row, so the Roles picker shows the new role. -->
    {#if droppedBanner}
      <AlertBanner variant="warn">{droppedBanner}</AlertBanner>
    {/if}

    <!--
      Shown only when Discord actually refused the premium rename in this guild.
      Discord freezes a bot's permissions into its role at install, so a server
      that added Bagel before the bot asked for Change Nickname keeps the old
      grant forever: the premium avatar applies, the name does not, and nothing
      self-heals until the streamer re-authorizes.
    -->
    {#if needsReauth}
      <AlertBanner variant="warn">
        {t('discord.reauthNeeded')}
        {#snippet action()}
          <ButtonLink variant="secondary" href="/discord/connect" data-sveltekit-reload>
            {t('discord.reauthCta')}
          </ButtonLink>
        {/snippet}
      </AlertBanner>
    {/if}

    <div class="shell reveal" style="--i:0">
      <GuildHeader
        guildId={data.guildId}
        guilds={data.guilds ?? []}
        name={guild.name}
        memberCount={guild.memberCount}
        {pillState}
        sinceMs={data.status?.sinceMs ?? 0}
      />
      <GuildNav guildId={data.guildId} />
    </div>

    {@render children()}
  {/if}
</section>

<style>
  /* One .screen for the whole section: the sub-pages render blocks into it, so
     a page that opened its own would nest two entrance animations and double
     the column gap. */
  .screen {
    display: flex;
    flex-direction: column;
    gap: 18px;
  }
  .shell {
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding-bottom: 4px;
    border-bottom: 1px solid var(--glass-border);
  }
</style>
