<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Chip, Icon, getI18n } from '@bagel/kit';

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

<input
  class="bb-input bb-input--fill"
  placeholder={t('commandEditor.aliasPlaceholder')}
  bind:value={draft}
  onkeydown={onKey}
  onblur={commit}
/>
{#if aliases.length}
  <div class="aliases">
    {#each aliases as a (a)}
      <Chip tone="muted" class="alias" onclick={() => remove(a)} aria-label={t('commandEditor.removeAlias', { name: a })}>
        <span>{a}</span>
        <Icon name="x" size={11} />
      </Chip>
    {/each}
  </div>
{/if}

<style>
  .aliases { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
  .aliases :global(.alias) { padding: 5px 10px; }
  .aliases :global(.alias svg) {
    width: 0;
    opacity: 0;
    transition: width var(--bb-dur-fast, 140ms) ease, opacity var(--bb-dur-fast, 140ms) ease;
  }
  .aliases :global(.alias:hover),
  .aliases :global(.alias:focus-visible) {
    color: #cf8a78;
    background: rgba(176, 90, 70, 0.16);
    border-color: rgba(176, 90, 70, 0.45);
    outline: none;
  }
  .aliases :global(.alias:hover svg),
  .aliases :global(.alias:focus-visible svg) {
    width: 11px;
    opacity: 1;
  }
</style>
