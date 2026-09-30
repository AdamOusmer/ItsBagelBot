<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { AlertBanner, ButtonLink, Card, Chip, Heading, Text } from '@bagel/ui/svelte';
  import { getI18n, droppedPinNotice } from '@bagel/kit';
  import GuildHeader from '$lib/components/discord/GuildHeader.svelte';
  import GuildNav from '$lib/components/discord/GuildNav.svelte';
  import { SLOT_LABEL_KEYS } from '$lib/discord/guild-fields';
  import { guildInfoOf, needsReauthOf, pillStateOf } from '$lib/discord/guild-view';
  import '$lib/components/discord/guild-forms.css';

  let { data, children } = $props();
  const { t } = getI18n();

  const guild = $derived(guildInfoOf(data));
  const needsReauth = $derived(needsReauthOf(data));
  const pillState = $derived(pillStateOf(data));

  const droppedPins = $derived(droppedPinNotice(data.droppedPins ?? []));
  const droppedBanner = $derived(
    droppedPins.length === 0
      ? ''
      : t('discord.roles.droppedPins', { slots: droppedPins.map((slot) => t(SLOT_LABEL_KEYS[slot])).join(', ') })
  );
</script>

<section class="screen active dc">
  {#if data.locked}
    <section class="block reveal" style="--i:0" aria-labelledby="dc-locked-h">
      <Heading level={6} as="h2" variant="title" id="dc-locked-h" class="block-title">{t('modules.betaLocked')}</Heading>
      <Card>
        <p class="lead"><Chip as="span" on>{t('modules.betaChip')}</Chip></p>
        <Text size="sm" tone="muted" class="hint">{t('modules.betaLockedBody')}</Text>
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
      <AlertBanner tone="warning">{t('discord.connectedLivedIn')}</AlertBanner>
    {/if}

    {#if droppedBanner}
      <AlertBanner tone="warning">{droppedBanner}</AlertBanner>
    {/if}

    {#if needsReauth}
      <AlertBanner tone="warning">
        {t('discord.reauthNeeded')}
        {#snippet actions()}
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
        iconUrl={guild.iconUrl}
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
    border-bottom: 1px solid var(--bb-glass-border);
  }
</style>
