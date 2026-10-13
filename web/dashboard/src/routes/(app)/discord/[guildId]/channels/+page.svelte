<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import { getI18n } from '@bagel/kit';
  import GuildForm from '$lib/components/discord/GuildForm.svelte';
  import LogOptions from '$lib/components/discord/LogOptions.svelte';
  import VoiceOptions from '$lib/components/discord/VoiceOptions.svelte';
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
  <AlertBanner tone="warning">{t('discord.layoutUnavailable')}</AlertBanner>
{/if}

<GuildForm {draft} id="dc-channels-h" title={t('discord.channels.title')} hint={t('discord.channels.help')}>
  <h3 class="group">{t('discord.channels.groupAnnounce')}</h3>
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="liveChannelId"
    label={t('discord.channels.liveLabel')}
    help={t('discord.channels.liveHelp')}
    options={textChannels}
    prefix="#"
  />
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="clipsChannelId"
    label={t('discord.channels.clipsLabel')}
    help={t('discord.channels.clipsHelp')}
    options={textChannels}
    prefix="#"
  />

  <h3 class="group">{t('discord.channels.groupCommunity')}</h3>
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="welcomeChannelId"
    label={t('discord.channels.welcomeLabel')}
    help={t('discord.channels.welcomeHelp')}
    options={textChannels}
    prefix="#"
  />

  <VoiceOptions {draft} {voiceChannels} {categories} />
  <LogOptions {draft} {textChannels} {voiceChannels} />

  <h3 class="group">{t('discord.channels.groupSubs')}</h3>
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="subsChannelId"
    label={t('discord.channels.subsLabel')}
    help={t('discord.channels.subsHelp')}
    options={textChannels}
    prefix="#"
  />
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="subsCategoryId"
    label={t('discord.channels.subsCategoryLabel')}
    help={t('discord.channels.subsCategoryHelp')}
    options={categories}
  />

  <h3 class="group">{t('discord.channels.groupVip')}</h3>
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="vipChannelId"
    label={t('discord.channels.vipLabel')}
    help={t('discord.channels.vipHelp')}
    options={textChannels}
    prefix="#"
  />
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="vipCategoryId"
    label={t('discord.channels.vipCategoryLabel')}
    help={t('discord.channels.vipCategoryHelp')}
    options={categories}
  />
</GuildForm>
