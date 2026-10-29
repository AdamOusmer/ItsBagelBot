<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { DISCORD_MODULES, getI18n } from '@bagel/kit';
  import DiscordStateTag from '#lib/components/discord/DiscordStateTag.svelte';
  import ModuleRow from '#lib/components/discord/ModuleRow.svelte';
  import { CLOSE_KEYS } from '#lib/discord/guild-fields';
  import { botOnlineOf, layoutDownOf, pillStateOf } from '#lib/discord/guild-view';
  import { moduleStateOf } from '#lib/discord/module-view';

  let { data } = $props();
  const { t } = getI18n();

  const rows = $derived(DISCORD_MODULES.map((def) => ({ def, status: moduleStateOf(def, data.stored[def.id]) })));
  const botOnline = $derived(botOnlineOf(data));
  const pillState = $derived(pillStateOf(data));
  const layoutDown = $derived(layoutDownOf(data.layout));
  const closeKey = $derived(CLOSE_KEYS[data.status?.lastCloseCode ?? 0]);
</script>

{#if !data.found}
  <AlertBanner tone="warning">
    {t('discord.overview.notSetUp')}
    {#snippet actions()}
      <ButtonLink variant="secondary" href="/discord/{data.guildId}/settings">
        {t('discord.overview.notSetUpCta')}
      </ButtonLink>
    {/snippet}
  </AlertBanner>
{/if}

{#if data.modulesDegraded}
  <AlertBanner>{t('discord.degraded')}</AlertBanner>
{/if}

<section class="block reveal" style="--i:1" aria-labelledby="dc-status-h">
  <Heading level={6} as="h2" variant="title" id="dc-status-h" class="block-title">{t('discord.status.title')}</Heading>
  <Card>
    <FactList layout="inline">
      <Fact term={t('discord.overview.botState')}>
        <DiscordStateTag state={pillState} />
      </Fact>
      <Fact term={t('discord.status.resumes')}>{data.status?.sessionResumes ?? 0}</Fact>
      <Fact term={t('discord.overview.modulesOn')}>{rows.filter((r) => r.status !== 'off').length} / {rows.length}</Fact>
    </FactList>

    {#if !botOnline && closeKey}
      <Text size="sm" tone="muted" class="hint state">{t(closeKey)}</Text>
    {:else if !botOnline}
      <Text size="sm" tone="muted" class="hint state">{t('discord.status.reconnecting')}</Text>
    {/if}

    {#if layoutDown}
      <Text size="sm" tone="muted" class="hint state">{t('discord.layoutUnavailable')}</Text>
    {/if}
  </Card>
</section>

<section class="block reveal" style="--i:2" aria-labelledby="dc-modules-h">
  <Heading level={6} as="h2" variant="title" id="dc-modules-h" class="block-title">{t('discord.overview.modulesTitle')}</Heading>
  <Text size="sm" tone="muted" class="hint">{t('discord.overview.modulesHelp')}</Text>
  <ul class="list">
    {#each rows as row (row.def.id)}
      <li><ModuleRow def={row.def} guildId={data.guildId} status={row.status} /></li>
    {/each}
  </ul>
</section>

<style>
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
  }
</style>
