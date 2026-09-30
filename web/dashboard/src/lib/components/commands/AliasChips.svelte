<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import { getI18n } from '@bagel/kit';

  const { t } = getI18n();

  let {
    aliases = $bindable<string[]>([]),
    draft = $bindable(''),
    commandName = ''
  }: {
    aliases: string[];
    draft: string;
    commandName?: string;
  } = $props();

  export function commit(): void {
    const a = draft.trim();
    draft = '';
    if (!a) return;
    const key = a.toLowerCase();
    if (key === commandName.trim().toLowerCase()) return;
    if (aliases.some((x) => x.toLowerCase() === key)) return;
    aliases = [...aliases, a];
  }

  function remove(alias: string) {
    aliases = aliases.filter((a) => a !== alias);
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      commit();
    } else if (e.key === 'Backspace' && draft === '' && aliases.length) {
      aliases = aliases.slice(0, -1);
    }
  }
</script>

<Input
  fill
  placeholder={t('commandEditor.aliasPlaceholder')}
  bind:value={draft}
  onkeydown={onKey}
  onblur={commit}
/>
{#if aliases.length}
  <div class="aliases">
    {#each aliases as a (a)}
      <Chip tone="muted" onclick={() => remove(a)} aria-label={t('commandEditor.removeAlias', { name: a })}>
        <span>{a}</span>
        <Icon name="x" size={11} />
      </Chip>
    {/each}
  </div>
{/if}

<style>
  .aliases { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
</style>
