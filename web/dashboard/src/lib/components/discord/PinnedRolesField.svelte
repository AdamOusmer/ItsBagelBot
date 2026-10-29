<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { encodePinnedRoles, getI18n, parsePinnedRoles, type DiscordConfig, type PinnedSlot } from '@bagel/kit';
  import type { ModuleDraft } from '#lib/discord/module-draft.svelte';
  import { SLOT_LABEL_KEYS } from '#lib/discord/guild-fields';
  import FieldNote from './FieldNote.svelte';

  let {
    draft,
    label,
    help
  }: { draft: ModuleDraft; label: string; help: string } = $props();

  const { t } = getI18n();

  const SLOT_FIELDS: { slot: PinnedSlot; field: keyof DiscordConfig }[] = [
    { slot: 'owner', field: 'ownerRoleId' },
    { slot: 'leadMod', field: 'leadModRoleId' },
    { slot: 'mods', field: 'modsRoleId' },
    { slot: 'vip', field: 'vipRoleId' },
    { slot: 'subscriber', field: 'subscriberRoleId' },
    { slot: 'regulars', field: 'regularsRoleId' }
  ];

  const pins = $derived(parsePinnedRoles(draft.config.pinnedRoles));

  const isPinned = (slot: PinnedSlot, field: keyof DiscordConfig): boolean =>
    draft.config[field] !== '' && pins[slot] === draft.config[field];

  function toggle(slot: PinnedSlot, field: keyof DiscordConfig) {
    const next = { ...pins };
    if (isPinned(slot, field)) delete next[slot];
    else if (draft.config[field]) next[slot] = draft.config[field];
    draft.set('pinnedRoles', encodePinnedRoles(next));
  }
</script>

<div class="setting-row stacked">
  <span class="tr-text">
    <Text as="span" size="sm">{label}</Text>
    <Text as="span" size="xs" tone="muted" id="dch-pinnedRoles">{help}</Text>
  </span>
  <div class="chips">
    {#each SLOT_FIELDS as row (row.slot)}
      <Chip
        pressed={isPinned(row.slot, row.field)}
        disabled={draft.config[row.field] === ''}
        aria-describedby="dch-pinnedRoles"
        onclick={() => toggle(row.slot, row.field)}
      >
        {t(SLOT_LABEL_KEYS[row.slot])}
      </Chip>
    {/each}
  </div>
  <FieldNote invalid={draft.invalid} field="pinnedRoles" />
</div>
