<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import VisuallyHidden from '@bagel/ui/svelte/VisuallyHidden.svelte';
  import {
    getI18n,
    encodeNameList,
    parseNameList,
    CATEGORY_NAME_MAX,
    type DiscordConfig
  } from '@bagel/kit';
  import GuildForm from '$lib/components/discord/GuildForm.svelte';
  import FieldNote from '$lib/components/discord/FieldNote.svelte';
  import SwitchRow from '$lib/components/discord/SwitchRow.svelte';
  import { createGuildDraft } from '$lib/discord/guild-draft.svelte';
  import { ANNOUNCEMENT_FIELDS } from '$lib/discord/guild-fields';

  let { data } = $props();
  const { t } = getI18n();

  const draft = createGuildDraft({ data: () => data, fields: ANNOUNCEMENT_FIELDS, t });

  const switches = $derived([
    { field: 'liveEnabled' as const, label: t('discord.announcements.liveLabel'), help: t('discord.announcements.liveHelp'), defaultOn: true },
    { field: 'clipsEnabled' as const, label: t('discord.announcements.clipsLabel'), help: t('discord.announcements.clipsHelp'), defaultOn: true }
  ]);

  const allowList = $derived(parseNameList(draft.config.categoryAllow));
  const denyList = $derived(parseNameList(draft.config.categoryDeny));
  let allowDraft = $state('');
  let denyDraft = $state('');

  function addName(field: keyof DiscordConfig, raw: string): boolean {
    const value = raw.trim();
    if (value === '' || value.length > CATEGORY_NAME_MAX) return false;
    const current = parseNameList(draft.config[field]);
    if (current.some((n) => n.toLowerCase() === value.toLowerCase())) return false;
    const next = encodeNameList([...current, value]);
    if (next === draft.config[field]) return false;
    draft.set(field, next);
    return true;
  }

  function removeName(field: keyof DiscordConfig, name: string) {
    draft.set(field, encodeNameList(parseNameList(draft.config[field]).filter((n) => n !== name)));
    removedNote = t('discord.announcements.chipRemoved', { name });
  }

  let removedNote = $state('');

  function commitAllow() {
    if (addName('categoryAllow', allowDraft)) allowDraft = '';
  }
  function commitDeny() {
    if (addName('categoryDeny', denyDraft)) denyDraft = '';
  }

  function addOnEnter(e: KeyboardEvent, commit: () => void) {
    if (e.key !== 'Enter') return;
    e.preventDefault();
    commit();
  }
</script>

{#snippet chipList(field: keyof DiscordConfig, names: string[])}
  <div class="chips">
    {#each names as name (name)}
      <Chip as="span" pressed>
        {name}
        <button type="button" class="chip-remove" aria-label={t('discord.announcements.chipRemove', { name })} onclick={() => removeName(field, name)}>
          <span class="chip-x" aria-hidden="true">×</span>
        </button>
      </Chip>
    {/each}
    {#if names.length === 0}
      <Text as="span" size="xs" tone="muted">{t('discord.announcements.chipEmpty')}</Text>
    {/if}
  </div>
{/snippet}

<VisuallyHidden as="p" role="status" aria-live="polite">{removedNote}</VisuallyHidden>

<GuildForm {draft} id="dc-posts-h" title={t('discord.announcements.postsTitle')} hint={t('discord.announcements.postsHelp')}>
  {#each switches as row (row.field)}
    <SwitchRow {draft} invalid={draft.invalid} {...row} />
  {/each}

  <div class="setting-row stacked">
    <span class="tr-text">
      <Text as="span" size="sm">{t('discord.announcements.allowLabel')}</Text>
      <Text as="span" size="xs" tone="muted" id="dch-allow">{t('discord.announcements.allowTag')}</Text>
    </span>
    {@render chipList('categoryAllow', allowList)}
    <span class="adder">
      <Input
        fill
        aria-describedby="dch-allow"
        aria-label={t('discord.announcements.allowLabel')}
        maxlength={CATEGORY_NAME_MAX}
        placeholder={t('discord.announcements.allowPlaceholder')}
        bind:value={allowDraft}
        onkeydown={(e: KeyboardEvent) => addOnEnter(e, commitAllow)}
      />
      <Button variant="secondary" onclick={commitAllow}>{t('discord.announcements.chipAdd')}</Button>
    </span>
    <FieldNote invalid={draft.invalid} field="categoryAllow" />
  </div>

  <div class="setting-row stacked">
    <span class="tr-text">
      <Text as="span" size="sm">{t('discord.announcements.denyLabel')}</Text>
      <Text as="span" size="xs" tone="muted" id="dch-deny">{t('discord.announcements.denyTag')}</Text>
    </span>
    {@render chipList('categoryDeny', denyList)}
    <span class="adder">
      <Input
        fill
        aria-describedby="dch-deny"
        aria-label={t('discord.announcements.denyLabel')}
        maxlength={CATEGORY_NAME_MAX}
        placeholder={t('discord.announcements.denyPlaceholder')}
        bind:value={denyDraft}
        onkeydown={(e: KeyboardEvent) => addOnEnter(e, commitDeny)}
      />
      <Button variant="secondary" onclick={commitDeny}>{t('discord.announcements.chipAdd')}</Button>
    </span>
    <FieldNote invalid={draft.invalid} field="categoryDeny" />
  </div>
</GuildForm>
