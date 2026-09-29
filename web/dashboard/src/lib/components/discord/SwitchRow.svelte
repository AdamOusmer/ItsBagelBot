<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { alertOff, alertOn, type DiscordConfig, type RefusedFields } from '@bagel/kit';
  import UiSwitchRow from '@bagel/ui/svelte/SwitchRow.svelte';
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

<div class="setting-switch">
  <UiSwitchRow
    control="end"
    {label}
    hint={help}
    hintId="dcs-{field}"
    checked={on}
    onchange={(v) => draft.setFlag(field, v)}
  >
    {#snippet note()}<FieldNote {invalid} {field} />{/snippet}
  </UiSwitchRow>
</div>
