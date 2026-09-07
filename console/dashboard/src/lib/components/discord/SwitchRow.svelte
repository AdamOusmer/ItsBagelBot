<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // One flag row.
  //
  // `defaultOn` is not decoration: half of these flags are ON unless explicitly
  // turned off (alertOn) and half are OFF unless explicitly turned on
  // (alertOff), and reading a blank field as "off" for the first group would
  // draw a module as stopped while it is posting.
  import { Switch, alertOff, alertOn, type DiscordConfig, type RefusedFields } from '@bagel/shared';
  import type { GuildDraft } from '$lib/discord/guild-draft.svelte';
  import FieldNote from './FieldNote.svelte';

  let {
    draft,
    invalid,
    field,
    label,
    help,
    defaultOn
  }: {
    draft: GuildDraft;
    invalid: RefusedFields;
    field: keyof DiscordConfig;
    label: string;
    help: string;
    defaultOn: boolean;
  } = $props();

  const on = $derived(defaultOn ? alertOn(draft.config[field]) : alertOff(draft.config[field]));
</script>

<div class="setting-row">
  <span class="tr-text">
    <span class="tr-label">{label}</span>
    <span class="tr-help" id="dcs-{field}">{help}</span>
  </span>
  <Switch {label} describedby="dcs-{field}" checked={on} onchange={(v) => draft.setFlag(field, v)} />
  <FieldNote {invalid} {field} />
</div>
