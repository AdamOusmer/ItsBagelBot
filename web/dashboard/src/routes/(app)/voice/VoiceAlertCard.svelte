<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { getI18n } from '@bagel/kit/i18n/context';
  import { chipsFor } from '@bagel/kit/variables';
  import { alertVoiceKeys, MAX_SPEECH_RUNES, VOICE_STYLES, type VoiceAlertKind } from '@bagel/kit/voice-plan';
  import type { SelectOption } from '@bagel/ui/lib/select';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import ResponseEditor from '#lib/components/commands/ResponseEditor.svelte';
  import ReplyTokenList from '#lib/components/variables/ReplyTokenList.svelte';

  let { kind, config = $bindable(), options, minBits, resetKey }: {
    kind: VoiceAlertKind;
    config: Record<string, string>;
    options: SelectOption[];
    minBits: number;
    resetKey: number;
  } = $props();

  const { t, locale } = getI18n();
  const numbers = new Intl.NumberFormat(locale);
  const keys = $derived(alertVoiceKeys(kind));
  const surface = $derived({ module: 'alerts', reply: kind });
  const label = $derived(t(`voice.kind.${kind}`));
  const enabled = $derived(config[keys.enabled] === 'true');
  const styles = Object.keys(VOICE_STYLES).map((value) => ({
    value,
    label: t(`voice.style.${value}`),
    description: t(`voice.styleDescription.${value}`)
  }));
  const pause = '<short pause>';
</script>

<Card as="section" aria-labelledby="voice-kind-{kind}">
  <div class="kind-head">
    <Heading as="h3" level={6} id="voice-kind-{kind}">
      <bb-fit><span data-fit="">{label}</span></bb-fit>
    </Heading>
    <div class="kind-toggle">
      <span class="off-label" class:visible={!enabled} aria-hidden="true">{t('commandEditor.off')}</span>
      <Switch checked={enabled} {label} onCheckedChange={(checked) => (config[keys.enabled] = String(checked))} />
    </div>
    <input type="hidden" name={keys.enabled} value={config[keys.enabled]} />
  </div>

  <div class="kind-body" class:muted={!enabled} inert={!enabled}>
    {#if kind === 'cheer'}
      <div class="threshold">
        <Text size="sm" as="div" tone="muted">
          <bb-fit><span data-fit="">{t('voice.cheerThreshold', { n: numbers.format(minBits) })}</span></bb-fit>
        </Text>
      </div>
    {/if}
    <div>
      <Field label={t('voice.spokenMessage')} fitCopy>
        {#key resetKey}
          <ResponseEditor name={keys.message} bind:value={config[keys.message]}
            {surface} placeholder={t(`voice.template.${kind}`)} maxlength={MAX_SPEECH_RUNES}>
            {#snippet extraActions(insert)}
              <Chip tone="muted" aria-label={t('voice.insertPause')} title={t('voice.insertPause')} onclick={() => insert(pause)}>
                <bb-fit><span data-fit="">{pause}</span></bb-fit>
              </Chip>
            {/snippet}
          </ResponseEditor>
        {/key}
      </Field>
      <ReplyTokenList chips={chipsFor(surface)} />
    </div>
    <Field label={t('voice.styleLabel')} fitCopy>
      <Select name={keys.style} label={t('voice.styleLabel')} options={styles} bind:value={config[keys.style]} fill />
    </Field>
    <Field label={t('voice.voiceOverride')} fitCopy>
      <Select name={keys.voice} label={t('voice.voiceOverride')} bind:value={config[keys.voice]} {options} searchable fill />
    </Field>
  </div>
</Card>

<style>
  .kind-head {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: var(--bb-space-2);
    margin-block-end: var(--bb-space-3);
  }
  .kind-toggle {
    display: flex;
    align-items: center;
    gap: var(--bb-space-2);
  }
  .off-label {
    color: var(--bb-muted);
    font-size: var(--bb-text-xs);
    opacity: 0;
  }
  .off-label.visible { opacity: 1; }
  .kind-body {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--bb-space-3);
  }
  .threshold { block-size: 1.25rem; }
  .kind-body.muted { opacity: 0.72; }
  @media (prefers-reduced-motion: no-preference) {
    .kind-body, .off-label { transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo); }
  }
</style>
