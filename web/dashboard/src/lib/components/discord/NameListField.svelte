<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import VisuallyHidden from '@bagel/ui/svelte/VisuallyHidden.svelte';
  import { CATEGORY_NAME_MAX, encodeNameList, getI18n, parseNameList, type DiscordConfig } from '@bagel/kit';
  import type { ModuleDraft } from '#lib/discord/module-draft.svelte';
  import FieldNote from './FieldNote.svelte';

  let {
    draft,
    field,
    label,
    help,
    placeholder
  }: {
    draft: ModuleDraft;
    field: keyof DiscordConfig;
    label: string;
    help: string;
    placeholder: string;
  } = $props();

  const { t } = getI18n();

  const names = $derived(parseNameList(draft.config[field]));
  let adding = $state('');
  let removedNote = $state('');

  function add() {
    const value = adding.trim();
    if (value === '' || value.length > CATEGORY_NAME_MAX) return;
    const next = encodeNameList([...names, value]);
    if (next === draft.config[field]) return;
    draft.set(field, next);
    adding = '';
  }

  function remove(name: string) {
    draft.set(field, encodeNameList(names.filter((n) => n !== name)));
    removedNote = t('discord.module.chipRemoved', { name });
  }

  function addOnEnter(e: KeyboardEvent) {
    if (e.key !== 'Enter') return;
    e.preventDefault();
    add();
  }
</script>

<VisuallyHidden as="p" role="status" aria-live="polite">{removedNote}</VisuallyHidden>

<div class="setting-row stacked">
  <span class="tr-text">
    <Text as="span" size="sm">{label}</Text>
    <Text as="span" size="xs" tone="muted" id="dch-{field}">{help}</Text>
  </span>
  <div class="chips">
    {#each names as name (name)}
      <Chip as="span" pressed>
        {name}
        <button type="button" class="chip-remove" aria-label={t('discord.module.chipRemove', { name })} onclick={() => remove(name)}>
          <span class="chip-x" aria-hidden="true">×</span>
        </button>
      </Chip>
    {/each}
    {#if names.length === 0}
      <Text as="span" size="xs" tone="muted">{t('discord.module.chipEmpty')}</Text>
    {/if}
  </div>
  <span class="adder">
    <Input
      fill
      aria-describedby="dch-{field}"
      aria-label={label}
      maxlength={CATEGORY_NAME_MAX}
      {placeholder}
      bind:value={adding}
      onkeydown={addOnEnter}
    />
    <Button variant="secondary" onclick={add}>{t('discord.module.chipAdd')}</Button>
  </span>
  <FieldNote invalid={draft.invalid} {field} />
</div>
