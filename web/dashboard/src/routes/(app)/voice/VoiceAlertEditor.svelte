<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { untrack } from 'svelte';
  import { moduleDef, MOD } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { chipsFor } from '@bagel/kit/variables';
  import { alertVoiceKeys, MAX_SPEECH_RUNES, VOICE_STYLES, type VoiceAlertKind } from '@bagel/kit/voice-plan';
  import type { SelectOption } from '@bagel/ui/lib/select';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import SaveStatus, { type SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import ChatPreview from '#lib/components/commands/ChatPreview.svelte';
  import ResponseEditor from '#lib/components/commands/ResponseEditor.svelte';
  import ReplyTokenList from '#lib/components/variables/ReplyTokenList.svelte';

  let { kind, values, options, status, onChange, onMessage, onFlush }: {
    kind: VoiceAlertKind;
    values: Record<string, string>;
    options: SelectOption[];
    status: Record<string, SaveState>;
    onChange: (key: string, value: string) => void;
    onMessage: (key: string, value: string) => void;
    onFlush: () => void;
  } = $props();

  const { t, locale } = getI18n();
  const numbers = new Intl.NumberFormat(locale);
  const keys = $derived(alertVoiceKeys(kind));
  const surface = $derived({ module: 'alerts', reply: kind });
  const samples = $derived(Object.fromEntries((moduleDef(MOD.alerts)?.replies.find((reply) => reply.key === kind)?.tokens ?? [])
    .map((token) => [token.name, token.sample])));
  const styles = Object.keys(VOICE_STYLES).map((value) => ({
    value,
    label: t(`voice.style.${value}`),
    description: t(`voice.styleDescription.${value}`)
  }));
  const pause = '<short pause>';

  let message = $state(untrack(() => values[keys.message] ?? ''));
  $effect(() => {
    const next = message;
    untrack(() => {
      if (next !== values[keys.message]) onMessage(keys.message, next);
    });
  });
</script>

<div class="editor">
  {#if kind === 'cheer'}
    <div class="threshold">
      <Text size="sm" as="div" tone="muted">
        <bb-fit><span data-fit="">{t('voice.cheerThreshold', { n: numbers.format(Number(values.ttsMinBits ?? 0)) })}</span></bb-fit>
      </Text>
    </div>
  {/if}
  <Field label={t('voice.spokenMessage')} fitCopy>
    <ResponseEditor name={keys.message} bind:value={message} {surface} placeholder={t(`voice.template.${kind}`)}
      maxlength={MAX_SPEECH_RUNES} onblur={onFlush}>
      {#snippet extraActions(insert)}
        <Chip tone="muted" aria-label={t('voice.insertPause')} title={t('voice.insertPause')} onclick={() => insert(pause)}>
          <bb-fit><span data-fit="">{pause}</span></bb-fit>
        </Chip>
      {/snippet}
    </ResponseEditor>
  </Field>
  <div class="message-state"><SaveStatus state={status[keys.message] ?? 'idle'} /></div>

  <ChatPreview kind="reply" name="" showViewer={false} tag={t('voice.rehearsal')} {samples}
    response={message.trim() || t(`voice.template.${kind}`)} />

  <ReplyTokenList chips={chipsFor(surface)} />

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
  .message-state {
    display: flex;
    justify-content: flex-end;
    block-size: 1.25rem;
    margin-block-start: calc(-1 * var(--bb-space-2));
  }
  .pickers {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--bb-space-3);
  }
</style>
