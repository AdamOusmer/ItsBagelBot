<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { LIVE_COLOR_HEX, getI18n, normalizeHex, type DiscordConfig } from '@bagel/kit';
  import type { ModuleDraft } from '#lib/discord/module-draft.svelte';
  import FieldNote from './FieldNote.svelte';

  let {
    draft,
    field,
    label,
    help
  }: { draft: ModuleDraft; field: keyof DiscordConfig; label: string; help: string } = $props();

  const { t } = getI18n();

  const SWATCHES = [LIVE_COLOR_HEX, '#52b788', '#5865f2', '#dfe4e9', '#b05a46', '#8a7cc9'] as const;
  const color = $derived(normalizeHex(draft.config[field]));
</script>

<div class="setting-row">
  <label class="tr-text" for="dc-{field}">
    <Text as="span" size="sm">{label}</Text>
    <Text as="span" size="xs" tone="muted">{help}</Text>
  </label>
  <span class="colors">
    {#each SWATCHES as swatch (swatch)}
      <button
        type="button"
        class="swatch {color === swatch ? 'on' : ''}"
        style="background: {swatch}"
        aria-label={t('discord.module.swatchLabel', { hex: swatch })}
        aria-pressed={color === swatch}
        onclick={() => draft.set(field, swatch)}
      ></button>
    {/each}
    <Input
      id="dc-{field}"
      type="color"
      value={color}
      oninput={(e: Event & { currentTarget: HTMLInputElement }) => draft.set(field, normalizeHex(e.currentTarget.value))}
    />
  </span>
  <FieldNote invalid={draft.invalid} {field} />
</div>
