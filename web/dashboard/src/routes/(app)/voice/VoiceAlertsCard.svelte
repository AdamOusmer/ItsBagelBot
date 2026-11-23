<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '@bagel/ui/styles/elements/collapse.css';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { alertVoiceKeys, VOICE_ALERT_KINDS, type VoiceAlertKind } from '@bagel/kit/voice-plan';
  import type { SelectOption } from '@bagel/ui/lib/select';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Divider from '@bagel/ui/svelte/Divider.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import SaveStatus, { type SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { styleOptions } from '#lib/components/voice/voice-options';

  let { values, options, statusOf, onChange }: {
    values: Record<string, string>;
    options: SelectOption[];
    statusOf: (kind: VoiceAlertKind) => SaveState;
    onChange: (key: string, value: string) => void;
  } = $props();

  const { t, locale } = getI18n();
  const numbers = new Intl.NumberFormat(locale);
  const styles = styleOptions(t);

  const summary = (kind: VoiceAlertKind) => kind === 'cheer'
    ? t('voice.cheerThreshold', { n: numbers.format(Number(values.ttsMinBits ?? 0)) })
    : t('voice.subMessageOnly');
</script>

<Card as="section" flush aria-labelledby="voice-alerts">
  <div class="section-head">
    <Heading level={6} as="h2" variant="eyebrow" id="voice-alerts">
      <bb-fit><span data-fit="">{t('voice.alertsTitle')}</span></bb-fit>
    </Heading>
    <Text as="p" size="xs" tone="muted">{t('voice.alertsHint')}</Text>
  </div>
  <ul class="list" aria-labelledby="voice-alerts">
    {#each VOICE_ALERT_KINDS as kind (kind)}
      {@const keys = alertVoiceKeys(kind)}
      {@const on = values[keys.enabled] === 'true'}
      {@const label = t(`voice.kind.${kind}`)}
      <li>
        <Divider flush />
        <div class="alert-row">
          <span class="alert-copy">
            <Heading level={6} as="span" id="voice-kind-{kind}">{label}</Heading>
            <Text as="span" size="xs" tone="muted">{summary(kind)}</Text>
          </span>
          <SaveStatus state={statusOf(kind)} />
          <Switch checked={on} label={t('modules.toggleAria', { label })}
            onCheckedChange={(checked) => onChange(keys.enabled, String(checked))} />
        </div>
        <div class="bb-collapse" class:is-open={on} aria-hidden={!on} inert={!on}>
          <div>
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
        </div>
      </li>
    {/each}
  </ul>
</Card>

<style>
  .section-head { display: flex; flex-direction: column; gap: var(--bb-space-1); padding: 12px 18px; }
  .list { list-style: none; margin: 0; padding: 0; }
  .alert-row { display: flex; align-items: center; gap: 12px; padding: 14px 18px; }
  .alert-copy { display: flex; flex-direction: column; gap: 3px; min-width: 0; margin-right: auto; }
  .pickers {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--bb-space-3);
    padding: 0 18px 16px;
  }
  @media (max-width: 600px) {
    .pickers { grid-template-columns: minmax(0, 1fr); }
  }
</style>
