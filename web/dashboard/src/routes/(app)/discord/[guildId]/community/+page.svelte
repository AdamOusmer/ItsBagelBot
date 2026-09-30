<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Text } from '@bagel/ui/svelte';
  import { getI18n } from '@bagel/kit';
  import GuildForm from '$lib/components/discord/GuildForm.svelte';
  import SwitchRow from '$lib/components/discord/SwitchRow.svelte';
  import { createGuildDraft } from '$lib/discord/guild-draft.svelte';
  import { COMMUNITY_FIELDS } from '$lib/discord/guild-fields';

  let { data } = $props();
  const { t } = getI18n();

  const draft = createGuildDraft({ data: () => data, fields: COMMUNITY_FIELDS, t });

  const switches = $derived([
    { field: 'welcomeEnabled' as const, label: t('discord.community.welcomeLabel'), help: t('discord.community.welcomeHelp'), defaultOn: true },
    { field: 'goodbyeEnabled' as const, label: t('discord.community.goodbyeLabel'), help: t('discord.community.goodbyeHelp'), defaultOn: false },
    { field: 'voiceEnabled' as const, label: t('discord.community.voiceLabel'), help: t('discord.community.voiceHelp'), defaultOn: true },
    { field: 'logsEnabled' as const, label: t('discord.community.logsLabel'), help: t('discord.community.logsHelp'), defaultOn: true },
    { field: 'levelsEnabled' as const, label: t('discord.community.levelsLabel'), help: t('discord.community.levelsHelp'), defaultOn: true },
    {
      field: 'linkGuardEnabled' as const,
      label: t('discord.community.linkGuardLabel'),
      help: t('discord.community.linkGuardHelp'),
      defaultOn: false
    },
    {
      field: 'subscribersEnabled' as const,
      label: t('discord.community.subscribersLabel'),
      help: t('discord.community.subscribersHelp'),
      defaultOn: false
    }
  ]);
</script>

<GuildForm {draft} id="dc-community-h" title={t('discord.community.title')} hint={t('discord.community.help')}>
  {#each switches as row (row.field)}
    <SwitchRow {draft} invalid={draft.invalid} {...row} />
  {/each}
  <Text size="sm" tone="muted" class="hint">{t('discord.community.tierRolesHelp')}</Text>
</GuildForm>
