<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Input from '@bagel/ui/svelte/Input.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import {
    getI18n,
    voicePrivacy,
    VOICE_NAME_MAX,
    VOICE_PRIVACY_MODES,
    VOICE_USER_LIMIT_MAX,
    VOICE_USER_LIMIT_MIN
  } from '@bagel/kit';
  import type { DiscordEntry } from '$lib/server/discord-store';
  import type { GuildDraft } from '$lib/discord/guild-draft.svelte';
  import ChannelPicker from './ChannelPicker.svelte';
  import FieldNote from './FieldNote.svelte';

  let {
    draft,
    voiceChannels,
    categories
  }: { draft: GuildDraft; voiceChannels: DiscordEntry[]; categories: DiscordEntry[] } = $props();

  const { t } = getI18n();

  const privacyOptions = $derived([
    { value: VOICE_PRIVACY_MODES[0], label: t('discord.channels.voicePrivacyOpen') },
    { value: VOICE_PRIVACY_MODES[1], label: t('discord.channels.voicePrivacyLocked') },
    { value: VOICE_PRIVACY_MODES[2], label: t('discord.channels.voicePrivacyHidden') }
  ]);
</script>

<h3 class="group">{t('discord.channels.groupVoice')}</h3>
<ChannelPicker
  {draft}
  invalid={draft.invalid}
  field="voiceHubId"
  label={t('discord.channels.voiceHubLabel')}
  help={t('discord.channels.voiceHubHelp')}
  options={voiceChannels}
/>
<ChannelPicker
  {draft}
  invalid={draft.invalid}
  field="voiceCategoryId"
  label={t('discord.channels.voiceCategoryLabel')}
  help={t('discord.channels.voiceCategoryHelp')}
  options={categories}
/>

<div class="setting-row">
  <label class="tr-text" for="dc-voiceNameTemplate">
    <Text as="span" size="sm">{t('discord.channels.voiceNameLabel')}</Text>
    <Text as="span" size="xs" tone="muted">{t('discord.channels.voiceNameHelp')}</Text>
  </label>
  <Input
    id="dc-voiceNameTemplate"
    maxlength={VOICE_NAME_MAX}
    placeholder={t('discord.channels.voiceNamePlaceholder')}
    value={draft.config.voiceNameTemplate}
    oninput={(e: Event & { currentTarget: HTMLInputElement }) => draft.set('voiceNameTemplate', e.currentTarget.value)}
  />
  <FieldNote invalid={draft.invalid} field="voiceNameTemplate" />
</div>

<div class="setting-row">
  <label class="tr-text" for="dc-voiceUserLimit">
    <Text as="span" size="sm">{t('discord.channels.voiceLimitLabel')}</Text>
    <Text as="span" size="xs" tone="muted">{t('discord.channels.voiceLimitHelp')}</Text>
  </label>
  <Input
    id="dc-voiceUserLimit"
    type="number"
    inputmode="numeric"
    min={VOICE_USER_LIMIT_MIN}
    max={VOICE_USER_LIMIT_MAX}
    placeholder={String(VOICE_USER_LIMIT_MIN)}
    value={draft.config.voiceUserLimit}
    oninput={(e: Event & { currentTarget: HTMLInputElement }) => draft.set('voiceUserLimit', e.currentTarget.value)}
  />
  <FieldNote invalid={draft.invalid} field="voiceUserLimit" />
</div>

<div class="setting-row">
  <span class="tr-text">
    <Text as="span" size="sm">{t('discord.channels.voicePrivacyLabel')}</Text>
    <Text as="span" size="xs" tone="muted">{t('discord.channels.voicePrivacyHelp')}</Text>
  </span>
  <SegmentedControl
    class="bb-tabs--even seg-fixed"
    options={privacyOptions}
    label={t('discord.channels.voicePrivacyLabel')}
    bind:value={() => voicePrivacy(draft.config), (v) => draft.set('voicePrivacy', v)}
  />
  <FieldNote invalid={draft.invalid} field="voicePrivacy" />
</div>
