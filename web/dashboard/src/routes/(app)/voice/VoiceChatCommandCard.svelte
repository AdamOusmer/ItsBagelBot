<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '@bagel/ui/styles/elements/collapse.css';
  import { getI18n, PERMS, tPerm } from '@bagel/kit';
  import { MAX_SPEECH_RUNES } from '@bagel/kit/voice-plan';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Divider from '@bagel/ui/svelte/Divider.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import SaveStatus, { type SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import SwitchRow from '@bagel/ui/svelte/SwitchRow.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import VoiceOverrideFields from '#lib/components/voice/VoiceOverrideFields.svelte';

  let { values, status, onChange }: {
    values: Record<string, string>;
    status: Record<string, SaveState>;
    onChange: (key: string, value: string) => void;
  } = $props();

  const { t } = getI18n();
  const on = $derived(values.enabled === 'true');
  const master = $derived({ key: 'enabled', label: t('voice.chat.enable'), hint: t('voice.chat.enableHint') });
  const toggles = $derived([
    { key: 'prefixName', label: t('voice.chat.prefix'), hint: undefined },
    { key: 'replyDenied', label: t('voice.chat.replyDenied'), hint: t('voice.chat.replyDeniedHint') }
  ]);
  const numberFields = $derived([
    { key: 'userCooldown', label: t('voice.chat.userCooldown'), min: 0, max: 86_400 },
    { key: 'cooldown', label: t('voice.chat.cooldown'), min: 0, max: 86_400 },
    { key: 'maxChars', label: t('voice.chat.maxChars'), min: 1, max: MAX_SPEECH_RUNES }
  ]);
  const perms = PERMS.map((perm) => ({ value: perm, label: tPerm(t, perm) }));
  const stateOf = (key: string) => status[key] ?? 'idle';

  function blurOnEnter(event: KeyboardEvent & { currentTarget: HTMLInputElement }) {
    if (event.key === 'Enter') event.currentTarget.blur();
  }
</script>

{#snippet toggleRow(toggle: { key: string; label: string; hint: string | undefined })}
  <Divider flush />
  <div class="setting-row toggle">
    <SwitchRow control="end" fitCopy label={toggle.label} hint={toggle.hint} hintId={toggle.hint ? `voice-chat-${toggle.key}-hint` : undefined}
      checked={values[toggle.key] === 'true'} busy={stateOf(toggle.key) === 'saving'}
      onCheckedChange={(checked) => onChange(toggle.key, String(checked))}>
      {#snippet status()}<SaveStatus state={stateOf(toggle.key)} />{/snippet}
    </SwitchRow>
  </div>
{/snippet}

<Card as="section" flush aria-labelledby="voice-chat">
  <div class="section-head">
    <Heading level={6} as="h2" variant="eyebrow" id="voice-chat">
      <bb-fit><span data-fit="">{t('voice.chat.title')}</span></bb-fit>
    </Heading>
    <span class="autosave"><Text as="span" size="xs" tone="muted">{t('modules.autoSaveNote')}</Text></span>
  </div>
  {@render toggleRow(master)}
  <div class="bb-collapse" class:is-open={on} aria-hidden={!on} inert={!on}>
  <div>
  {#each toggles as toggle (toggle.key)}
    {@render toggleRow(toggle)}
  {/each}
  <Divider flush />
  <div class="setting-row">
    <label class="tr-text" for="voice-chat-permission"><Text as="span" size="sm">{t('voice.chat.permission')}</Text></label>
    <SaveStatus state={stateOf('permission')} />
    <div class="setting-picker">
      <Select id="voice-chat-permission" label={t('voice.chat.permission')} value={values.permission || 'everyone'} options={perms} fill
        onchange={(event) => onChange('permission', event.currentTarget.value)} />
    </div>
  </div>
  {#each numberFields as field (field.key)}
    <Divider flush />
    <div class="setting-row">
      <label class="tr-text" for="voice-chat-{field.key}"><Text as="span" size="sm">{field.label}</Text></label>
      <SaveStatus state={stateOf(field.key)} />
      <Input id="voice-chat-{field.key}" type="number" min={field.min} max={field.max} step={1} required value={values[field.key]}
        onchange={(event: Event & { currentTarget: HTMLInputElement }) => onChange(field.key, event.currentTarget.value)}
        onkeydown={blurOnEnter} />
    </div>
  {/each}
  <Divider flush />
  <div class="setting-row overrides">
    <VoiceOverrideFields style={values.style} voice={values.voice} onChange={(key, value) => onChange(key, value)} />
  </div>
  </div>
  </div>
</Card>

<style>
  .section-head { display: flex; align-items: center; gap: 12px; padding: 12px 18px; }
  .autosave { margin-left: auto; }
  .tr-text { display: flex; flex-direction: column; gap: 3px; margin-right: auto; min-width: 0; }
  .setting-row {
    --input-w: min(260px, 44vw);
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 14px 18px;
  }
  .setting-row.toggle, .setting-row.overrides { display: block; }
  .setting-picker { width: min(260px, 44vw); }
  @media (max-width: 560px) {
    .setting-row { --input-w: 100%; flex-wrap: wrap; }
    .setting-picker { width: 100%; }
  }
</style>
