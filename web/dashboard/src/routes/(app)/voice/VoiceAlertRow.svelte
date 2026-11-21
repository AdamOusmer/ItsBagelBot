<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '@bagel/ui/styles/elements/row-body.css';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { VoiceAlertKind } from '@bagel/kit/voice-plan';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import SaveStatus, { type SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';

  let { kind, index, minBits, enabled, status, expanded, onExpand, onToggle }: {
    kind: VoiceAlertKind;
    index: number;
    minBits: number;
    enabled: boolean;
    status: SaveState;
    expanded: boolean;
    onExpand: () => void;
    onToggle: (enabled: boolean) => void;
  } = $props();

  const { t, locale } = getI18n();
  const numbers = new Intl.NumberFormat(locale);
  const label = $derived(t(`voice.kind.${kind}`));
  const summary = $derived(kind === 'cheer'
    ? t('voice.cheerThreshold', { n: numbers.format(minBits) })
    : t('voice.subMessageOnly'));
</script>

<div class="row-wrap" class:flash-save={status === 'saved'}>
  <ManagementRow selected={expanded} {expanded} controls="voice-editor" disabled={!enabled} onSelect={onExpand}>
    {#snippet primary()}
      <span class="prow bb-row-body bb-row-body--stack">
        <span class="bb-row-body__idx" aria-hidden="true"><Label mono as="span">{String(index).padStart(2, '0')}</Label></span>
        <span class="alert">
          <Heading level={6} as="span" id="voice-kind-{kind}">{label}</Heading>
          <Text as="span" size="xs" tone="muted" truncate>{summary}</Text>
        </span>
        <span class="state"><SaveStatus state={status} /></span>
      </span>
    {/snippet}
    {#snippet actions()}
      <Switch checked={enabled} label={t('modules.toggleAria', { label })} onCheckedChange={onToggle} />
    {/snippet}
  </ManagementRow>
</div>

<style>
  .prow { --row-cols: 28px minmax(0, 1fr) auto; }
  .alert { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
  .state { min-width: 0; }

  @media (max-width: 760px) {
    .prow { grid-template-columns: minmax(0, 1fr) auto; }
    .state { display: none; }
  }
</style>
