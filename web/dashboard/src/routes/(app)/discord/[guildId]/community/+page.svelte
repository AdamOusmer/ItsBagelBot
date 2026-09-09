<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Community ops: one declared list, one loop. Every row here is a flag, and
  // the channel each one posts to is picked on the Channels page.
  import { getI18n } from '@bagel/kit';
  import GuildForm from '$lib/components/discord/GuildForm.svelte';
  import SwitchRow from '$lib/components/discord/SwitchRow.svelte';
  import { createGuildDraft } from '$lib/discord/guild-draft.svelte';
  import { COMMUNITY_FIELDS } from '$lib/discord/guild-fields';

  let { data } = $props();
  const { t } = getI18n();

  const draft = createGuildDraft({ data: () => data, fields: COMMUNITY_FIELDS, t });

  const switches = $derived([
    { field: 'welcomeEnabled' as const, label: t('discord.welcomeLabel'), help: t('discord.welcomeHelp'), defaultOn: true },
    { field: 'goodbyeEnabled' as const, label: t('discord.goodbyeLabel'), help: t('discord.goodbyeHelp'), defaultOn: false },
    { field: 'voiceEnabled' as const, label: t('discord.voiceLabel'), help: t('discord.voiceHelp'), defaultOn: true },
    { field: 'logsEnabled' as const, label: t('discord.logsLabel'), help: t('discord.logsHelp'), defaultOn: true },
    { field: 'levelsEnabled' as const, label: t('discord.levelsLabel'), help: t('discord.levelsHelp'), defaultOn: true },
    {
      field: 'linkGuardEnabled' as const,
      label: t('discord.linkGuardLabel'),
      help: t('discord.linkGuardHelp'),
      defaultOn: false
    },
    {
      field: 'subscribersEnabled' as const,
      label: t('discord.subscribersLabel'),
      help: t('discord.subscribersHelp'),
      defaultOn: false
    }
  ]);
</script>

<GuildForm {draft} id="dc-community-h" title={t('discord.communityTitle')} hint={t('discord.communityHelp')}>
  {#each switches as row (row.field)}
    <SwitchRow {draft} invalid={draft.invalid} {...row} />
  {/each}
  <p class="hint">{t('discord.tierRolesHelp')}</p>
</GuildForm>
