<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // One "pick a channel, category or role" row.
  //
  // The empty option is "Not set" rather than a blank: a picker with nothing
  // chosen is a real, supported state (that module simply does not post), and a
  // blank first row reads as a list that failed to load.
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
    /** '#' for text channels, '' for voice channels and categories. */
    prefix?: string;
  } = $props();

  const { t } = getI18n();

  /** Announcement channels are offered by name plus a marker, so picking one is
   *  a choice rather than a surprise: Discord rate-limits them hard and fans
   *  every post out to following servers. */
  function optionLabel(opt: DiscordEntry): string {
    return opt.type === 5 ? `${opt.name} ${t('discord.announcementTag')}` : opt.name;
  }
</script>

<div class="setting-row">
  <label class="tr-text" for="dc-{field}">
    <span class="tr-label">{label}</span>
    <span class="tr-help" id="dch-{field}">{help}</span>
  </label>
  <select
    id="dc-{field}"
    class="setting-input"
    aria-describedby="dch-{field}"
    disabled={options.length === 0}
    value={draft.config[field]}
    onchange={(e) => draft.set(field, e.currentTarget.value)}
  >
    <option value="">{t('discord.notSet')}</option>
    {#each options as opt (opt.id)}
      <option value={opt.id}>{prefix}{optionLabel(opt)}</option>
    {/each}
  </select>
  <FieldNote {invalid} {field} />
</div>
