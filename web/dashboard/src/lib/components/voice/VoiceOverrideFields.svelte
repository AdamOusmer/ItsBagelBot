<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { getI18n } from '@bagel/kit/i18n/context';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import { styleOptions, voiceOptions } from './voice-options';

  let { style = $bindable(''), voice = $bindable(''), onChange }: {
    style: string;
    voice: string;
    onChange?: (key: 'style' | 'voice', value: string) => void;
  } = $props();

  const { t } = getI18n();
  const styles = [{ value: '', label: t('voice.styleDefault') }, ...styleOptions(t)];
  const voices = [{ value: '', label: t('voice.channelVoice') }, ...voiceOptions(t)];

  function pick(key: 'style' | 'voice', value: string) {
    if (key === 'style') style = value;
    else voice = value;
    onChange?.(key, value);
  }
</script>

<div class="pickers">
  <Field label={t('voice.styleLabel')} fitCopy>
    <Select label={t('voice.styleLabel')} options={styles} value={style} fill
      onchange={(event) => pick('style', event.currentTarget.value)} />
  </Field>
  <Field label={t('voice.voiceOverride')} fitCopy>
    <Select label={t('voice.voiceOverride')} options={voices} value={voice} searchable fill
      onchange={(event) => pick('voice', event.currentTarget.value)} />
  </Field>
</div>

<style>
  .pickers {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--bb-space-3);
  }
  @media (max-width: 600px) {
    .pickers { grid-template-columns: minmax(0, 1fr); }
  }
</style>
