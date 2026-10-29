<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Checkbox from '@bagel/ui/svelte/Checkbox.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { encodeIdList, getI18n, parseIdList, type DiscordConfig } from '@bagel/kit';
  import type { DiscordEntry } from '#lib/server/discord-store';
  import type { ModuleDraft } from '#lib/discord/module-draft.svelte';
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

  let query = $state('');
  const selected = $derived(parseIdList(draft.config[field]));
  const seeded = $derived(new Set(parseIdList(draft.saved[field])));
  const known = $derived(new Set(roles.map((r) => r.id)));
  const missing = $derived(selected.filter((id) => !known.has(id)));
  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase();
    const hits = roles.filter((r) => !q || r.name.toLowerCase().includes(q));
    return [...hits.filter((r) => seeded.has(r.id)), ...hits.filter((r) => !seeded.has(r.id))];
  });

  function toggle(id: string, on: boolean) {
    draft.set(field, encodeIdList(on ? [...selected, id] : selected.filter((r) => r !== id)));
  }
</script>

<fieldset class="setting-row stacked staff">
  <legend><Text as="span" size="sm">{label}</Text></legend>
  <Text as="span" size="xs" tone="muted" id="dch-{field}">{help}</Text>
  {#if roles.length === 0}
    <Text as="span" size="xs" tone="muted">{t('discord.module.roleListEmpty')}</Text>
  {:else}
    {#if missing.length}
      <Text as="span" size="xs" tone="muted" role="status">{t('discord.module.roleListMissingHelp')}</Text>
    {/if}
    {#if roles.length > 8}
      <SearchInput
        bind:value={query}
        placeholder={t('discord.module.roleListFilter')}
        aria-label={t('discord.module.roleListFilter')}
        clearLabel={t('common.searchClear')}
        autocomplete="off"
        fill
      />
    {/if}
    <div class="checks">
      {#each missing as id (id)}
        <Checkbox class="check" aria-describedby="dch-{field}" bind:checked={() => true, () => toggle(id, false)}>
          <Text as="span" size="sm" truncate>{t('discord.module.roleListMissing', { id })}</Text>
        </Checkbox>
      {/each}
      {#each visible as role (role.id)}
        <Checkbox
          class="check"
          aria-describedby="dch-{field}"
          bind:checked={() => selected.includes(role.id), (on) => toggle(role.id, on)}
        >
          <Text as="span" size="sm" truncate>@{role.name}</Text>
        </Checkbox>
      {/each}
    </div>
    {#if visible.length === 0}
      <Text as="span" size="xs" tone="muted">{t('discord.module.roleListNoMatch')}</Text>
    {/if}
  {/if}
  <FieldNote invalid={draft.invalid} {field} />
</fieldset>
