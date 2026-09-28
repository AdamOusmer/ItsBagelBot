<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { untrack } from 'svelte';
  import { Select } from '@bagel/kit';
  import { getI18n, type DiscordConfig, type RefusedFields } from '@bagel/kit';
  import type { DiscordEntry } from '$lib/server/discord-store';
  import type { GuildDraft } from '$lib/discord/guild-draft.svelte';
  import FieldNote from './FieldNote.svelte';

  let {
    draft,
    invalid,
    field,
    label,
    help,
    options,
    prefix = ''
  }: {
    draft: GuildDraft;
    invalid: RefusedFields;
    field: keyof DiscordConfig;
    label: string;
    help: string;
    options: DiscordEntry[];
    prefix?: string;
  } = $props();

  const { t } = getI18n();

  const blocked = (opt: DiscordEntry | undefined): boolean => opt?.botCanSend === false;
  const savedBlocked = untrack(() => blocked(options.find((opt) => opt.id === String(draft.config[field] ?? ''))));
  const currentBlocked = $derived(blocked(options.find((opt) => opt.id === String(draft.config[field] ?? ''))));

  function optionLabel(opt: DiscordEntry): string {
    const name = opt.type === 5 ? `${opt.name} ${t('discord.announcementTag')}` : opt.name;
    const inCategory = opt.parentName ? `${name} · ${opt.parentName}` : name;
    return blocked(opt) ? `${inCategory} · ${t('discord.botCannotPost')}` : inCategory;
  }
</script>

<div class="setting-row">
  <label class="tr-text" for="dc-{field}">
    <span class="tr-label">{label}</span>
    <span class="tr-help" id="dch-{field}">{help}</span>
  </label>
  <div class="setting-picker">
    <Select
      fill
      searchable
      label={label}
      searchPlaceholder={t('common.searchOptions')}
      searchClearLabel={t('common.searchClear')}
      emptyLabel={t('common.selectNoMatch')}
      id="dc-{field}"
      aria-describedby="dch-{field}"
      disabled={options.length === 0}
      value={String(draft.config[field] ?? '')}
      onchange={(e) => draft.set(field, e.currentTarget.value)}
      options={[{ value: '', label: t('discord.notSet') }, ...options.map((opt) => ({ value: opt.id, label: `${prefix}${optionLabel(opt)}`, disabled: blocked(opt) }))]}
    />
  </div>
  {#if savedBlocked}
  <FieldNote {invalid} {field} warning={t('discord.savedChannelBlocked')} quiet={!currentBlocked} />
{:else}
  <FieldNote {invalid} {field} />
{/if}
</div>
