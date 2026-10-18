<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import VisuallyHidden from '@bagel/ui/svelte/VisuallyHidden.svelte';
  import { encodeIdList, getI18n, parseIdList, LOG_IGNORED_CHANNELS_MAX, type DiscordConfig } from '@bagel/kit';
  import type { DiscordEntry } from '$lib/server/discord-store';
  import type { ModuleDraft } from '$lib/discord/module-draft.svelte';
  import FieldNote from './FieldNote.svelte';

  let {
    draft,
    field,
    label,
    help,
    channels
  }: {
    draft: ModuleDraft;
    field: keyof DiscordConfig;
    label: string;
    help: string;
    channels: DiscordEntry[];
  } = $props();

  const { t } = getI18n();

  const picked = $derived(parseIdList(draft.config[field]));
  const full = $derived(picked.length >= LOG_IGNORED_CHANNELS_MAX);
  const addable = $derived(channels.filter((c) => !picked.includes(c.id)));
  const pickerOptions = $derived([
    { value: '', label: t('discord.module.channelListAdd') },
    ...addable.map((c) => ({ value: c.id, label: c.type === 2 ? c.name : `#${c.name}` }))
  ]);

  let removedNote = $state('');

  function nameOf(id: string): string {
    const hit = channels.find((c) => c.id === id);
    return hit ? hit.name : t('discord.module.channelListUnknown', { id });
  }

  function add(id: string) {
    if (id === '' || full || picked.includes(id)) return;
    draft.set(field, encodeIdList([...picked, id]));
  }

  function remove(id: string) {
    draft.set(field, encodeIdList(picked.filter((i) => i !== id)));
    removedNote = t('discord.module.channelListRemoved', { name: nameOf(id) });
  }
</script>

<VisuallyHidden as="p" role="status" aria-live="polite">{removedNote}</VisuallyHidden>

<div class="setting-row stacked">
  <span class="tr-text">
    <Text as="span" size="sm">{label}</Text>
    <Text as="span" size="xs" tone="muted" id="dch-{field}" class="seg-fixed-help">{help}</Text>
  </span>
  <div class="chips">
    {#each picked as id (id)}
      <Chip as="span" pressed>
        {nameOf(id)}
        <button type="button" class="chip-remove" aria-label={t('discord.module.channelListRemove', { name: nameOf(id) })} onclick={() => remove(id)}>
          <span class="chip-x" aria-hidden="true">×</span>
        </button>
      </Chip>
    {/each}
    {#if picked.length === 0}
      <Text as="span" size="xs" tone="muted">{t('discord.module.channelListEmpty')}</Text>
    {/if}
  </div>
  {#key picked.length}
    <div class="setting-picker">
      <Select
        fill
        searchable
        {label}
        searchPlaceholder={t('common.searchOptions')}
        searchClearLabel={t('common.searchClear')}
        emptyLabel={t('common.selectNoMatch')}
        id="dc-{field}"
        aria-describedby="dch-{field}"
        disabled={full || addable.length === 0}
        value=""
        onchange={(e) => add(e.currentTarget.value)}
        options={pickerOptions}
      />
    </div>
  {/key}
  <span style:visibility={full ? 'visible' : 'hidden'} aria-hidden={!full}>
    <Text as="span" size="xs" tone="muted">{t('discord.module.channelListFull')}</Text>
  </span>
  <FieldNote invalid={draft.invalid} {field} />
</div>
