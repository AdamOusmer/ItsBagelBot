<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n, type DiscordConfig } from '@bagel/kit';
  import type { DiscordEntry } from '$lib/server/discord-store';
  import type { ModuleDraft } from '$lib/discord/module-draft.svelte';
  import FieldNote from './FieldNote.svelte';

  let {
    draft,
    field,
    label,
    help,
    roles
  }: {
    draft: ModuleDraft;
    field: keyof DiscordConfig;
    label: string;
    help: string;
    roles: DiscordEntry[];
  } = $props();

  const { t } = getI18n();
</script>

<div class="setting-row">
  <label class="tr-text" for="dc-{field}">
    <Text as="span" size="sm">{label}</Text>
    <Text as="span" size="xs" tone="muted" id="dch-{field}">{help}</Text>
  </label>
  <span class="setting-picker">
    <Select
      fill
      searchable
      {label}
      searchPlaceholder={t('common.searchOptions')}
      searchClearLabel={t('common.searchClear')}
      emptyLabel={t('common.selectNoMatch')}
      id="dc-{field}"
      aria-describedby="dch-{field}"
      disabled={roles.length === 0}
      value={String(draft.config[field] ?? '')}
      onchange={(e) => draft.set(field, e.currentTarget.value)}
      options={[{ value: '', label: t('discord.notSet') }, ...roles.map((opt) => ({ value: opt.id, label: `@${opt.name}` }))]}
    />
  </span>
  <FieldNote invalid={draft.invalid} {field} />
</div>
