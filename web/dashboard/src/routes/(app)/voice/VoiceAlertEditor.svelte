<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { getI18n } from '@bagel/kit/i18n/context';
  import { alertVoiceKeys, VOICE_STYLES, type VoiceAlertKind } from '@bagel/kit/voice-plan';
  import type { SelectOption } from '@bagel/ui/lib/select';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import ChatPreview from '#lib/components/commands/ChatPreview.svelte';

  let { kind, values, options, onChange }: {
    kind: VoiceAlertKind;
    values: Record<string, string>;
    options: SelectOption[];
    onChange: (key: string, value: string) => void;
  } = $props();

  const { t, locale } = getI18n();
  const numbers = new Intl.NumberFormat(locale);
  const keys = $derived(alertVoiceKeys(kind));
  const note = $derived(kind === 'cheer'
    ? t('voice.cheerThreshold', { n: numbers.format(Number(values.ttsMinBits ?? 0)) })
    : t('voice.subMessageOnly'));
  const styles = Object.keys(VOICE_STYLES).map((value) => ({
    value,
    label: t(`voice.style.${value}`),
    description: t(`voice.styleDescription.${value}`)
  }));
</script>

<div class="editor">
  <div class="threshold">
    <Text size="sm" as="div" tone="muted">
      <bb-fit><span data-fit="">{note}</span></bb-fit>
    </Text>
  </div>

  <ChatPreview kind="reply" name="" showViewer={false} tag={t('voice.rehearsal')} response={t(`voice.sample.${kind}`)} />

  <div class="pickers">
    <Field label={t('voice.styleLabel')} fitCopy>
      <Select name={keys.style} label={t('voice.styleLabel')} options={styles} value={values[keys.style]} fill
        onchange={(event) => onChange(keys.style, event.currentTarget.value)} />
    </Field>
    <Field label={t('voice.voiceOverride')} fitCopy>
      <Select name={keys.voice} label={t('voice.voiceOverride')} value={values[keys.voice]} {options} searchable fill
        onchange={(event) => onChange(keys.voice, event.currentTarget.value)} />
    </Field>
  </div>
</div>

<style>
  .editor {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--bb-space-3);
    padding: 4px 2px 2px;
  }
  .threshold { block-size: 1.25rem; }
  .pickers {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--bb-space-3);
  }
  @media (max-width: 600px) {
    .pickers { grid-template-columns: minmax(0, 1fr); }
  }
</style>
