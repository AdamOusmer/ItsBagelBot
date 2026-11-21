<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { getI18n } from '@bagel/kit';
  import type { VoiceAlertKind } from '@bagel/kit/voice-plan';
  import SaveStatus, { type SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import Divider from '@bagel/ui/svelte/Divider.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import PremiumMark from '#lib/components/shared/PremiumMark.svelte';

  export type VoiceControl = {
    kind: VoiceAlertKind;
    checked: boolean;
    allowed: boolean;
    status: SaveState;
    onToggle: (checked: boolean) => void;
  };

  let { voice, disabled = false }: { voice: VoiceControl; disabled?: boolean } = $props();

  const { t } = getI18n();
  const on = $derived(voice.allowed && voice.checked);
  const eventLabel = $derived(t(`modules.voiceEvent.${voice.kind}.label`));
</script>

<div class="voice-rule"><Divider flush /></div>
<div class="voice-line">
  <span class="voice-tag"><Tag tone="alpha">{t('modules.voiceEvent.tts')}</Tag></span>
  <span class="voice-copy" class:off={!on}>
    <Text as="span" size="sm">{t(`voice.sample.${voice.kind}`)}</Text>
    <Text as="span" size="xs" tone="muted">{t(`modules.voiceEvent.${voice.kind}.hint`)}</Text>
  </span>
  <span class="voice-controls">
    <PremiumMark />
    <span class="voice-status"><SaveStatus state={voice.status} compact /></span>
    <Switch checked={on} label={t('modules.voiceEvent.toggleAria', { event: eventLabel })}
      disabled={disabled || !voice.allowed} busy={voice.status === 'saving'} onCheckedChange={voice.onToggle} />
  </span>
</div>

<style>
  .voice-line {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: center;
    gap: var(--bb-space-3);
    padding-top: var(--bb-space-3);
  }
  .voice-rule { margin-top: var(--bb-space-2); }
  .voice-tag { display: inline-flex; }
  .voice-copy { display: grid; gap: 3px; min-width: 0; transition: opacity 0.15s ease; }
  .voice-copy.off { opacity: 0.45; }
  .voice-controls { display: inline-flex; align-items: center; gap: var(--bb-space-2); }
  .voice-status { width: 20px; }
  @media (prefers-reduced-motion: reduce) {
    .voice-copy { transition: none; }
  }
</style>
