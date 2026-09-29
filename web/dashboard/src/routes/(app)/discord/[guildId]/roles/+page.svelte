<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import {
    AlertBanner,
    Chip,
    Select,
    Text,
    encodePinnedRoles,
    getI18n,
    parsePinnedRoles,
    type DiscordConfig,
    type PinnedSlot
  } from '@bagel/kit';
  import GuildForm from '$lib/components/discord/GuildForm.svelte';
  import FieldNote from '$lib/components/discord/FieldNote.svelte';
  import SwitchRow from '$lib/components/discord/SwitchRow.svelte';
  import { createGuildDraft } from '$lib/discord/guild-draft.svelte';
  import { ROLE_FIELDS } from '$lib/discord/guild-fields';
  import { layoutDownOf, rolesOf } from '$lib/discord/guild-view';

  let { data } = $props();
  const { t } = getI18n();

  const draft = createGuildDraft({ data: () => data, fields: ROLE_FIELDS, t });

  const roles = $derived(rolesOf(data.layout));
  const layoutDown = $derived(layoutDownOf(data.layout));

  type RoleRow = { slot: PinnedSlot; field: keyof DiscordConfig; label: string; help: string };

  const staffRoles = $derived<RoleRow[]>([
    { slot: 'owner', field: 'ownerRoleId', label: t('discord.ownerRoleLabel'), help: t('discord.ownerRoleHelp') },
    { slot: 'leadMod', field: 'leadModRoleId', label: t('discord.leadModRoleLabel'), help: t('discord.leadModRoleHelp') },
    { slot: 'mods', field: 'modsRoleId', label: t('discord.modsRoleLabel'), help: t('discord.modsRoleHelp') }
  ]);
  const tierRoles = $derived<RoleRow[]>([
    { slot: 'vip', field: 'vipRoleId', label: t('discord.vipRoleLabel'), help: t('discord.vipRoleHelp') },
    {
      slot: 'subscriber',
      field: 'subscriberRoleId',
      label: t('discord.subscriberRoleLabel'),
      help: t('discord.subscriberRoleHelp')
    },
    {
      slot: 'regulars',
      field: 'regularsRoleId',
      label: t('discord.regularsRoleLabel'),
      help: t('discord.regularsRoleHelp')
    },
    { slot: 'member', field: 'memberRoleId', label: t('discord.memberRoleLabel'), help: t('discord.memberRoleHelp') }
  ]);

  const pins = $derived(parsePinnedRoles(draft.config.pinnedRoles));

  function isPinned(row: RoleRow): boolean {
    const id = draft.config[row.field];
    return id !== '' && pins[row.slot] === id;
  }

  function togglePin(row: RoleRow) {
    const next = { ...pins };
    if (isPinned(row)) delete next[row.slot];
    else if (draft.config[row.field]) next[row.slot] = draft.config[row.field];
    draft.set('pinnedRoles', encodePinnedRoles(next));
  }
</script>

{#snippet roleRow(row: RoleRow)}
  <div class="setting-row">
    <label class="tr-text" for="dc-{row.field}">
      <Text as="span" size="sm">{row.label}</Text>
      <Text as="span" size="xs" tone="muted" id="dch-{row.field}">{row.help}</Text>
    </label>
    <span class="role-controls">
      <Chip
        on={isPinned(row)}
        onclick={() => togglePin(row)}
        aria-pressed={isPinned(row)}
        disabled={draft.config[row.field] === ''}
      >
        {t('discord.pinnedChip')}
      </Chip>
      <span class="setting-picker">
        <Select
          fill
          searchable
          label={row.label}
          searchPlaceholder={t('common.searchOptions')}
          searchClearLabel={t('common.searchClear')}
          emptyLabel={t('common.selectNoMatch')}
          id="dc-{row.field}"
          aria-describedby="dch-{row.field}"
          disabled={roles.length === 0}
          value={String(draft.config[row.field] ?? '')}
          onchange={(e) => draft.set(row.field, e.currentTarget.value)}
          options={[{ value: '', label: t('discord.notSet') }, ...roles.map((opt) => ({ value: opt.id, label: `@${opt.name}` }))]}
        />
      </span>
    </span>
    <FieldNote invalid={draft.invalid} field={row.field} />
  </div>
{/snippet}

{#if layoutDown}
  <AlertBanner variant="warn">{t('discord.layoutUnavailable')}</AlertBanner>
{/if}

<GuildForm {draft} id="dc-roles-h" title={t('discord.rolesTitle')} hint={t('discord.rolesHelp')}>
  <h3 class="group">{t('discord.groupStaff')}</h3>
  {#each staffRoles as row (row.slot)}
    {@render roleRow(row)}
  {/each}

  <h3 class="group">{t('discord.groupTiers')}</h3>
  {#each tierRoles as row (row.slot)}
    {@render roleRow(row)}
  {/each}

  <SwitchRow
    {draft}
    invalid={draft.invalid}
    field="autoRoleEnabled"
    label={t('discord.autoRoleLabel')}
    help={t('discord.autoRoleHelp')}
    defaultOn
  />

  <Text size="sm" tone="muted" class="hint">{t('discord.pinHelp')}</Text>
</GuildForm>
