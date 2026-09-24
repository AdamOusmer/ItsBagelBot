<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { AlertBanner, getI18n } from '@bagel/kit';
  import GuildForm from '$lib/components/discord/GuildForm.svelte';
  import ChannelPicker from '$lib/components/discord/ChannelPicker.svelte';
  import { createGuildDraft } from '$lib/discord/guild-draft.svelte';
  import { CHANNEL_FIELDS } from '$lib/discord/guild-fields';
  import { categoriesOf, layoutDownOf, textChannelsOf, voiceChannelsOf } from '$lib/discord/guild-view';

  let { data } = $props();
  const { t } = getI18n();

  const draft = createGuildDraft({ data: () => data, fields: CHANNEL_FIELDS, t });

  const textChannels = $derived(textChannelsOf(data.layout));
  const voiceChannels = $derived(voiceChannelsOf(data.layout));
  const categories = $derived(categoriesOf(data.layout));
  const layoutDown = $derived(layoutDownOf(data.layout));
</script>

{#if layoutDown}
  <AlertBanner variant="warn">{t('discord.layoutUnavailable')}</AlertBanner>
{/if}

<GuildForm {draft} id="dc-channels-h" title={t('discord.channelsTitle')} hint={t('discord.channelsHelp')}>
  <h3 class="group">{t('discord.groupAnnounce')}</h3>
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="liveChannelId"
    label={t('discord.liveChannelLabel')}
    help={t('discord.liveChannelHelp')}
    options={textChannels}
    prefix="#"
  />
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="clipsChannelId"
    label={t('discord.clipsChannelLabel')}
    help={t('discord.clipsChannelHelp')}
    options={textChannels}
    prefix="#"
  />

  <h3 class="group">{t('discord.groupCommunity')}</h3>
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="welcomeChannelId"
    label={t('discord.welcomeChannelLabel')}
    help={t('discord.welcomeChannelHelp')}
    options={textChannels}
    prefix="#"
  />
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="voiceHubId"
    label={t('discord.voiceHubLabel')}
    help={t('discord.voiceHubHelp')}
    options={voiceChannels}
  />
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="logChannelId"
    label={t('discord.logChannelLabel')}
    help={t('discord.logChannelHelp')}
    options={textChannels}
    prefix="#"
  />

  <h3 class="group">{t('discord.groupSubs')}</h3>
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="subsChannelId"
    label={t('discord.subsChannelLabel')}
    help={t('discord.subsChannelHelp')}
    options={textChannels}
    prefix="#"
  />
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="subsCategoryId"
    label={t('discord.subsCategoryLabel')}
    help={t('discord.subsCategoryHelp')}
    options={categories}
  />

  <h3 class="group">{t('discord.groupVip')}</h3>
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="vipChannelId"
    label={t('discord.vipChannelLabel')}
    help={t('discord.vipChannelHelp')}
    options={textChannels}
    prefix="#"
  />
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="vipCategoryId"
    label={t('discord.vipCategoryLabel')}
    help={t('discord.vipCategoryHelp')}
    options={categories}
  />
</GuildForm>
