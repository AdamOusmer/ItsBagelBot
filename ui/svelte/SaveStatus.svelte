<script module lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getUiI18n } from './i18n';

  export type SaveState = 'idle' | 'saving' | 'saved' | 'live' | 'error';
</script>

<script lang="ts">
  import '../styles/elements/save-status.css';

  const i18n = getUiI18n();
  let {
    state = 'idle',
    compact = false,
    savingLabel = i18n.t('status.saving'),
    savedLabel = i18n.t('status.saved'),
    liveLabel = i18n.t('status.live'),
    errorLabel = i18n.t('status.failed'),
    class: className = '',
    ...rest
  }: {
    state?: SaveState;
    compact?: boolean;
    savingLabel?: string;
    savedLabel?: string;
    liveLabel?: string;
    errorLabel?: string;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const TONE: Record<SaveState, string> = {
    idle: '',
    saving: 'bb-tag--quiet',
    saved: 'bb-tag--alpha',
    live: 'bb-tag--live',
    error: 'bb-tag--error',
  };
  const MARK: Record<SaveState, string> = {
    idle: '',
    saving: 'bb-mark--dash',
    saved: '',
    live: '',
    error: 'bb-mark--hollow',
  };

  const labels: Record<SaveState, string> = $derived({
    idle: '',
    saving: savingLabel,
    saved: savedLabel,
    live: liveLabel,
    error: errorLabel,
  });

  const classes = $derived(
    ['bb-tag', TONE[state] || null, className || null].filter(Boolean).join(' '),
  );
</script>

{#if state !== 'idle'}
  <span class={classes} role="status" {...rest}><i
      class={['bb-mark', MARK[state] || null].filter(Boolean).join(' ')}
      aria-hidden="true"
    ></i>{#if !compact}{labels[state]}{/if}{#if state === 'saving'}<i
      class="bb-sweep bb-sweep--tan"
      aria-hidden="true"
    ></i>{/if}{#if state === 'live'}<i class="bb-sweep" aria-hidden="true"></i>{/if}</span
  >
{/if}
