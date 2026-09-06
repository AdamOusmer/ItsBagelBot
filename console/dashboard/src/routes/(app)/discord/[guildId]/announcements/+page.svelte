<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Stream posts: the two switches plus the category allow/deny lists that
  // decide which streams get announced at all.
  import { Button, Chip, getI18n, encodeNameList, parseNameList, CATEGORY_NAME_MAX, type DiscordConfig } from '@bagel/shared';
  import GuildForm from '$lib/components/discord/GuildForm.svelte';
  import FieldNote from '$lib/components/discord/FieldNote.svelte';
  import SwitchRow from '$lib/components/discord/SwitchRow.svelte';
  import { createGuildDraft } from '$lib/discord/guild-draft.svelte';
  import { ANNOUNCEMENT_FIELDS } from '$lib/discord/guild-fields';

  let { data } = $props();
  const { t } = getI18n();

  const draft = createGuildDraft({ data: () => data, fields: ANNOUNCEMENT_FIELDS, t });

  const switches = $derived([
    { field: 'liveEnabled' as const, label: t('discord.liveLabel'), help: t('discord.liveHelp'), defaultOn: true },
    { field: 'clipsEnabled' as const, label: t('discord.clipsLabel'), help: t('discord.clipsHelp'), defaultOn: true }
  ]);

  const allowList = $derived(parseNameList(draft.config.categoryAllow));
  const denyList = $derived(parseNameList(draft.config.categoryDeny));
  let allowDraft = $state('');
  let denyDraft = $state('');

  function addName(field: keyof DiscordConfig, raw: string): boolean {
    const value = raw.trim();
    if (value === '' || value.length > CATEGORY_NAME_MAX) return false;
    const next = encodeNameList([...parseNameList(draft.config[field]), value]);
    if (next === draft.config[field]) return false;
    draft.set(field, next);
    return true;
  }

  function removeName(field: keyof DiscordConfig, name: string) {
    draft.set(field, encodeNameList(parseNameList(draft.config[field]).filter((n) => n !== name)));
  }

  function commitAllow() {
    if (addName('categoryAllow', allowDraft)) allowDraft = '';
  }
  function commitDeny() {
    if (addName('categoryDeny', denyDraft)) denyDraft = '';
  }

  // Enter adds the chip instead of submitting the form: the input sits inside
  // the save form, so the native default would post a half-typed category and
  // lose it.
  function addOnEnter(e: KeyboardEvent, commit: () => void) {
    if (e.key !== 'Enter') return;
    e.preventDefault();
    commit();
  }
</script>

{#snippet chipList(field: keyof DiscordConfig, names: string[])}
  <div class="chips">
    {#each names as name (name)}
      <Chip on onclick={() => removeName(field, name)} aria-label={t('discord.chipRemove', { name })}>
        {name}<span class="chip-x" aria-hidden="true">×</span>
      </Chip>
    {/each}
    {#if names.length === 0}
      <span class="tr-help">{t('discord.chipEmpty')}</span>
    {/if}
  </div>
{/snippet}

<GuildForm {draft} id="dc-posts-h" title={t('discord.postsTitle')} hint={t('discord.postsHelp')}>
  {#each switches as row (row.field)}
    <SwitchRow {draft} invalid={draft.invalid} {...row} />
  {/each}

  <div class="setting-row stacked">
    <span class="tr-text">
      <span class="tr-label">{t('discord.allowLabel')}</span>
      <span class="tr-help" id="dch-allow">{t('discord.allowTag')}</span>
    </span>
    {@render chipList('categoryAllow', allowList)}
    <span class="adder">
      <input
        class="setting-input"
        aria-describedby="dch-allow"
        aria-label={t('discord.allowLabel')}
        maxlength={CATEGORY_NAME_MAX}
        placeholder={t('discord.allowPlaceholder')}
        bind:value={allowDraft}
        onkeydown={(e) => addOnEnter(e, commitAllow)}
      />
      <Button variant="secondary" icon="plus" onclick={commitAllow}>{t('discord.chipAdd')}</Button>
    </span>
    <FieldNote invalid={draft.invalid} field="categoryAllow" />
  </div>

  <div class="setting-row stacked">
    <span class="tr-text">
      <span class="tr-label">{t('discord.denyLabel')}</span>
      <span class="tr-help" id="dch-deny">{t('discord.denyTag')}</span>
    </span>
    {@render chipList('categoryDeny', denyList)}
    <span class="adder">
      <input
        class="setting-input"
        aria-describedby="dch-deny"
        aria-label={t('discord.denyLabel')}
        maxlength={CATEGORY_NAME_MAX}
        placeholder={t('discord.denyPlaceholder')}
        bind:value={denyDraft}
        onkeydown={(e) => addOnEnter(e, commitDeny)}
      />
      <Button variant="secondary" icon="plus" onclick={commitDeny}>{t('discord.chipAdd')}</Button>
    </span>
    <FieldNote invalid={draft.invalid} field="categoryDeny" />
  </div>
</GuildForm>
