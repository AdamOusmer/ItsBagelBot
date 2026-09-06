<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Which Discord role each Bagel slot means, and which of them setup must
  // adopt rather than create.
  import {
    AlertBanner,
    Chip,
    Switch,
    alertOn,
    encodePinnedRoles,
    getI18n,
    parsePinnedRoles,
    type DiscordConfig,
    type PinnedSlot
  } from '@bagel/shared';
  import GuildForm from '$lib/components/discord/GuildForm.svelte';
  import FieldNote from '$lib/components/discord/FieldNote.svelte';
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

  // Pinning a slot tells setup to adopt the role that is selected right now
  // instead of creating (or renaming) one by name on the next fill.
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
      <span class="tr-label">{row.label}</span>
      <span class="tr-help" id="dch-{row.field}">{row.help}</span>
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
      <select
        id="dc-{row.field}"
        class="setting-input"
        aria-describedby="dch-{row.field}"
        disabled={roles.length === 0}
        value={draft.config[row.field]}
        onchange={(e) => draft.set(row.field, e.currentTarget.value)}
      >
        <option value="">{t('discord.notSet')}</option>
        {#each roles as opt (opt.id)}
          <option value={opt.id}>@{opt.name}</option>
        {/each}
      </select>
    </span>
    <FieldNote invalid={draft.invalid} field={row.field} />
  </div>
{/snippet}

{#if layoutDown}
  <AlertBanner variant="warn" icon="server">{t('discord.layoutUnavailable')}</AlertBanner>
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

  <div class="setting-row">
    <span class="tr-text">
      <span class="tr-label">{t('discord.autoRoleLabel')}</span>
      <span class="tr-help" id="dcs-autoRole">{t('discord.autoRoleHelp')}</span>
    </span>
    <Switch
      label={t('discord.autoRoleLabel')}
      describedby="dcs-autoRole"
      checked={alertOn(draft.config.autoRoleEnabled)}
      onchange={(v) => draft.setFlag('autoRoleEnabled', v)}
    />
    <FieldNote invalid={draft.invalid} field="autoRoleEnabled" />
  </div>

  <p class="hint">{t('discord.pinHelp')}</p>
</GuildForm>
