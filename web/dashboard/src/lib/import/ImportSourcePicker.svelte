<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import IconTile from '@bagel/ui/svelte/IconTile.svelte';
  import RadioGroup from '@bagel/ui/svelte/RadioGroup.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import { IMPORT_SOURCES, type ImportSource } from '@bagel/kit';
  import { CHIP_LABEL_KEYS, IMPORT_STRATEGIES } from '@bagel/kit/importer/strategy';
  import { translate, type Locale } from '@bagel/kit/i18n';

  let { locale, source, label, onchoose, min, cols, terse = false, staggered = false, style, onpointerover, onpointerleave }: {
    locale: Locale;
    source: string;
    label: string;
    onchoose: (value: string) => void;
    min: string;
    cols?: number;
    terse?: boolean;
    staggered?: boolean;
    style?: string;
    onpointerover?: (e: PointerEvent) => void;
    onpointerleave?: () => void;
  } = $props();

  const t = (key: string, params?: Record<string, string | number>) => translate(locale, key, params);
  const importText = (text: string, params?: Record<string, string | number>) => translate(locale, `import.${text}`, params);

  const options = $derived(
    IMPORT_SOURCES.map((id) => {
      const s = IMPORT_STRATEGIES[id];
      return {
        value: id,
        label: s.label,
        description: importText(s.i18n.desc),
        meta: s.available ? t('import.tileCta') : undefined,
        disabled: !s.available
      };
    })
  );
</script>

<div class="tiles" {style}>
  <RadioGroup
    variant="cards"
    {min}
    {cols}
    {terse}
    name="source-pick"
    {label}
    value={source}
    {options}
    onValueChange={onchoose}
    class={staggered ? 'bb-stagger' : ''}
    {onpointerover}
    {onpointerleave}
  >
    {#snippet leading(option, on)}
      {@const s = IMPORT_STRATEGIES[option.value as ImportSource]}
      <IconTile picked={on}>{s.initials}</IconTile>
      {#if s.available}<Tag tone="pre">{t(CHIP_LABEL_KEYS[s.chip])}</Tag>{:else}<Tag tone="quiet">{t('import.chipSoon')}</Tag>{/if}
    {/snippet}
  </RadioGroup>
</div>

<style>
  .tiles { margin: 16px 0 4px; }

  @media (max-width: 760px), (max-height: 760px) {
    .tiles { margin: 8px 0 0; }
  }
</style>
