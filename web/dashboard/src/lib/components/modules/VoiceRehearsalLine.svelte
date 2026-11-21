<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { getI18n } from '@bagel/kit';
  import type { VoiceAlertKind } from '@bagel/kit/voice-plan';
  import SaveStatus, { type SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import PremiumMark from '#lib/components/shared/PremiumMark.svelte';

  export type VoiceControl = {
    kind: VoiceAlertKind;
    checked: boolean;
    allowed: boolean;
    status: SaveState;
    chatOn: boolean;
    onToggle: (checked: boolean) => void;
  };

  let { voice, disabled = false }: { voice: VoiceControl; disabled?: boolean } = $props();

  const { t } = getI18n();
  const on = $derived(voice.allowed && voice.checked);
  const eventLabel = $derived(t(`modules.voiceEvent.${voice.kind}.label`));
  const hint = $derived(voice.chatOn ? t(`modules.voiceEvent.${voice.kind}.hint`) : t('modules.voiceEvent.withoutChat'));
</script>

<div class="voice">
  <div class="voice-say" class:off={!on}>
    <div class="bb-chat__line">
      <span class="bb-chat__name bb-chat__name--bot">{t('modules.voiceEvent.tts')}</span>
      <span class="bb-chat__msg">{t(`voice.sample.${voice.kind}`)}</span>
    </div>
  </div>
  <div class="voice-controls">
    <PremiumMark />
    <span class="voice-hint"><Text as="span" size="xs" tone="muted">{hint}</Text></span>
    <span class="voice-toggle">
      <span class="voice-status"><SaveStatus state={voice.status} compact /></span>
      <Switch checked={on} label={t('modules.voiceEvent.toggleAria', { event: eventLabel })}
        disabled={disabled || !voice.allowed} busy={voice.status === 'saving'} onCheckedChange={voice.onToggle} />
    </span>
  </div>
</div>

<style>
  .voice { display: grid; gap: var(--bb-space-3); margin-top: var(--bb-space-3); }
  .voice-say { transition: opacity 0.15s ease; }
  .voice-say.off { opacity: 0.45; }
  .voice-controls {
    display: flex;
    align-items: center;
    gap: var(--bb-space-3);
    padding: var(--bb-space-1) var(--bb-space-2) var(--bb-space-2);
  }
  .voice-hint { flex: 1; min-width: 0; }
  .voice-toggle { display: inline-flex; align-items: center; gap: var(--bb-space-2); flex: none; }
  .voice-status { width: 20px; }
  @media (prefers-reduced-motion: reduce) {
    .voice-say { transition: none; }
  }
</style>
