<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import VisuallyHidden from '@bagel/ui/svelte/VisuallyHidden.svelte';
  import { encodeIdList, getI18n, parseIdList, LOG_IGNORED_CHANNELS_MAX } from '@bagel/kit';
  import type { DiscordEntry } from '$lib/server/discord-store';
  import type { GuildDraft } from '$lib/discord/guild-draft.svelte';
  import ChannelPicker from './ChannelPicker.svelte';
  import FieldNote from './FieldNote.svelte';
  import SwitchRow from './SwitchRow.svelte';

  let {
    draft,
    textChannels,
    voiceChannels
  }: { draft: GuildDraft; textChannels: DiscordEntry[]; voiceChannels: DiscordEntry[] } = $props();

  const { t } = getI18n();

  const toggles = $derived([
    { field: 'logMessagesEnabled' as const, label: t('discord.channels.logMessagesLabel'), help: t('discord.channels.logMessagesHelp'), defaultOn: true },
    { field: 'logMembersEnabled' as const, label: t('discord.channels.logMembersLabel'), help: t('discord.channels.logMembersHelp'), defaultOn: true },
    { field: 'logVoiceEnabled' as const, label: t('discord.channels.logVoiceLabel'), help: t('discord.channels.logVoiceHelp'), defaultOn: true },
    { field: 'logModerationEnabled' as const, label: t('discord.channels.logModerationLabel'), help: t('discord.channels.logModerationHelp'), defaultOn: true },
    { field: 'logChannelsEnabled' as const, label: t('discord.channels.logChannelsLabel'), help: t('discord.channels.logChannelsHelp'), defaultOn: true },
    { field: 'logRolesEnabled' as const, label: t('discord.channels.logRolesLabel'), help: t('discord.channels.logRolesHelp'), defaultOn: true },
    { field: 'logServerEnabled' as const, label: t('discord.channels.logServerLabel'), help: t('discord.channels.logServerHelp'), defaultOn: true }
  ]);

  const routed = $derived([
    { field: 'logMessagesChannelId' as const, label: t('discord.channels.logMessagesChannelLabel') },
    { field: 'logMembersChannelId' as const, label: t('discord.channels.logMembersChannelLabel') },
    { field: 'logVoiceChannelId' as const, label: t('discord.channels.logVoiceChannelLabel') },
    { field: 'logModerationChannelId' as const, label: t('discord.channels.logModerationChannelLabel') }
  ]);

  const ignorable = $derived([...textChannels, ...voiceChannels]);
  const ignored = $derived(parseIdList(draft.config.logIgnoredChannelIds));
  const full = $derived(ignored.length >= LOG_IGNORED_CHANNELS_MAX);
  const addable = $derived(ignorable.filter((c) => !ignored.includes(c.id)));
  const pickerOptions = $derived([
    { value: '', label: t('discord.channels.logIgnoredAdd') },
    ...addable.map((c) => ({ value: c.id, label: c.type === 2 ? c.name : `#${c.name}` }))
  ]);

  let removedNote = $state('');

  function nameOf(id: string): string {
    const hit = ignorable.find((c) => c.id === id);
    return hit ? hit.name : t('discord.channels.logIgnoredUnknown', { id });
  }

  function add(id: string) {
    if (id === '' || full || ignored.includes(id)) return;
    draft.set('logIgnoredChannelIds', encodeIdList([...ignored, id]));
  }

  function remove(id: string) {
    draft.set('logIgnoredChannelIds', encodeIdList(ignored.filter((i) => i !== id)));
    removedNote = t('discord.channels.logIgnoredRemoved', { name: nameOf(id) });
  }
</script>

<VisuallyHidden as="p" role="status" aria-live="polite">{removedNote}</VisuallyHidden>

<h3 class="group">{t('discord.channels.groupLogs')}</h3>
<ChannelPicker
  {draft}
  invalid={draft.invalid}
  field="logChannelId"
  label={t('discord.channels.logLabel')}
  help={t('discord.channels.logHelp')}
  options={textChannels}
  prefix="#"
/>
{#each routed as row (row.field)}
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field={row.field}
    label={row.label}
    help={t('discord.channels.logCategoryChannelHelp')}
    options={textChannels}
    prefix="#"
  />
{/each}

<h3 class="group">{t('discord.channels.logCategoriesTitle')}</h3>
<Text size="sm" tone="muted" class="hint">{t('discord.channels.logCategoriesHelp')}</Text>
{#each toggles as row (row.field)}
  <SwitchRow {draft} invalid={draft.invalid} {...row} />
{/each}
<SwitchRow
  {draft}
  invalid={draft.invalid}
  field="logIgnoreBots"
  label={t('discord.channels.logIgnoreBotsLabel')}
  help={t('discord.channels.logIgnoreBotsHelp')}
  defaultOn
/>

<div class="setting-row stacked">
  <span class="tr-text">
    <Text as="span" size="sm">{t('discord.channels.logIgnoredLabel')}</Text>
    <Text as="span" size="xs" tone="muted" id="dch-logIgnored" class="seg-fixed-help">{t('discord.channels.logIgnoredHelp', { max: LOG_IGNORED_CHANNELS_MAX })}</Text>
  </span>
  <div class="chips">
    {#each ignored as id (id)}
      <Chip as="span" pressed>
        {nameOf(id)}
        <button type="button" class="chip-remove" aria-label={t('discord.channels.logIgnoredRemove', { name: nameOf(id) })} onclick={() => remove(id)}>
          <span class="chip-x" aria-hidden="true">×</span>
        </button>
      </Chip>
    {/each}
    {#if ignored.length === 0}
      <Text as="span" size="xs" tone="muted">{t('discord.channels.logIgnoredEmpty')}</Text>
    {/if}
  </div>
  {#key ignored.length}
    <div class="setting-picker">
      <Select
        fill
        searchable
        label={t('discord.channels.logIgnoredLabel')}
        searchPlaceholder={t('common.searchOptions')}
        searchClearLabel={t('common.searchClear')}
        emptyLabel={t('common.selectNoMatch')}
        id="dc-logIgnoredChannelIds"
        aria-describedby="dch-logIgnored"
        disabled={full || addable.length === 0}
        value=""
        onchange={(e) => add(e.currentTarget.value)}
        options={pickerOptions}
      />
    </div>
  {/key}
  <span style:visibility={full ? 'visible' : 'hidden'} aria-hidden={!full}>
    <Text as="span" size="xs" tone="muted">{t('discord.channels.logIgnoredFull')}</Text>
  </span>
  <FieldNote invalid={draft.invalid} field="logIgnoredChannelIds" />
</div>
