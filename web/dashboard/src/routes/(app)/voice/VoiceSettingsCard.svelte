<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { getI18n } from '@bagel/kit/i18n/context';
  import { MAX_CLIP_SECONDS, MIN_CLIP_SECONDS, VOICE_CHARS_PER_SECOND } from '@bagel/kit/voice-plan';
  import type { SelectOption } from '@bagel/ui/lib/select';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Divider from '@bagel/ui/svelte/Divider.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import SaveStatus, { type SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import SwitchRow from '@bagel/ui/svelte/SwitchRow.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';

  let { values, status, options, onChange }: {
    values: Record<string, string>;
    status: Record<string, SaveState>;
    options: SelectOption[];
    onChange: (key: string, value: string) => void;
  } = $props();

  const { t, locale } = getI18n();
  const numbers = new Intl.NumberFormat(locale);
  const approxChars = $derived(numbers.format((Number(values.max_seconds) || 0) * VOICE_CHARS_PER_SECOND));
  const toggles = $derived([
    { key: 'enabled', label: t('voice.enable'), hint: t('voice.fallback') },
    { key: 'ttsLiveOnly', label: t('voice.liveOnly'), hint: undefined },
    { key: 'ttsReadMessages', label: t('voice.readMessages'), hint: t('voice.readMessagesHint') }
  ]);
  const numberFields = $derived([
    { key: 'ttsMinBits', label: t('voice.minBits'), hint: undefined, min: 0, max: Number.MAX_SAFE_INTEGER },
    { key: 'max_seconds', label: t('voice.maxLength'), hint: t('voice.maxLengthHint', { chars: approxChars }), min: MIN_CLIP_SECONDS, max: MAX_CLIP_SECONDS }
  ]);
  const stateOf = (key: string) => status[key] ?? 'idle';

  function blurOnEnter(event: KeyboardEvent & { currentTarget: HTMLInputElement }) {
    if (event.key === 'Enter') event.currentTarget.blur();
  }
</script>

<Card as="section" flush aria-labelledby="voice-settings">
  <div class="section-head">
    <Heading level={6} as="h2" variant="eyebrow" id="voice-settings">
      <bb-fit><span data-fit="">{t('voice.settingsTitle')}</span></bb-fit>
    </Heading>
    <span class="autosave"><Text as="span" size="xs" tone="muted">{t('modules.autoSaveNote')}</Text></span>
  </div>
  {#each toggles as toggle (toggle.key)}
    <Divider flush />
    <div class="setting-row toggle">
      <SwitchRow control="end" fitCopy label={toggle.label} hint={toggle.hint} hintId={toggle.hint ? `voice-${toggle.key}-hint` : undefined}
        checked={values[toggle.key] === 'true'} busy={stateOf(toggle.key) === 'saving'}
        onCheckedChange={(checked) => onChange(toggle.key, String(checked))}>
        {#snippet status()}<SaveStatus state={stateOf(toggle.key)} />{/snippet}
      </SwitchRow>
    </div>
  {/each}
  <Divider flush />
  <div class="setting-row">
    <label class="tr-text" for="voice-default">
      <Text as="span" size="sm">{t('voice.choose')}</Text>
    </label>
    <SaveStatus state={stateOf('voice')} />
    <div class="setting-picker">
      <Select id="voice-default" name="voice" label={t('voice.choose')} value={values.voice} {options} searchable required fill
        onchange={(event) => onChange('voice', event.currentTarget.value)} />
    </div>
  </div>
  {#each numberFields as field (field.key)}
    <Divider flush />
    <div class="setting-row">
      <label class="tr-text" for="voice-{field.key}">
        <Text as="span" size="sm">{field.label}</Text>
        {#if field.hint}<Text as="span" size="xs" tone="muted">{field.hint}</Text>{/if}
      </label>
      <SaveStatus state={stateOf(field.key)} />
      <Input id="voice-{field.key}" name={field.key} type="number" min={field.min} max={field.max} step={1} required
        value={values[field.key]} onchange={(event: Event & { currentTarget: HTMLInputElement }) => onChange(field.key, event.currentTarget.value)}
        onkeydown={blurOnEnter} />
    </div>
  {/each}
</Card>

<style>
  .section-head {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 18px;
  }
  .autosave { margin-left: auto; }
  .tr-text { display: flex; flex-direction: column; gap: 3px; margin-right: auto; min-width: 0; }
  .setting-row {
    --input-w: min(260px, 44vw);
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 14px 18px;
  }
  .setting-row.toggle { display: block; }
  .setting-picker { width: min(260px, 44vw); }
  @media (max-width: 560px) {
    .setting-row { --input-w: 100%; flex-wrap: wrap; }
    .setting-picker { width: 100%; }
  }
</style>
