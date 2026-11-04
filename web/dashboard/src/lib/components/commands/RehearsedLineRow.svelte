<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { translate, type Locale } from '@bagel/kit/i18n';
  import { getI18n, type RehearsedLine } from '@bagel/kit';
  import ChatBotName from './ChatBotName.svelte';
  import RehearsedSegments from './RehearsedSegments.svelte';

  let {
    line,
    index = 0,
    seed,
    active = false,
    locale = undefined as Locale | undefined
  }: {
    line: RehearsedLine;
    index?: number;
    seed: string;
    active?: boolean;
    locale?: Locale;
  } = $props();

  const i18n = getI18n();
  const t = (key: string, params?: Record<string, string | number>) => translate(locale ?? i18n.locale, key, params);

  const unknownTitle = $derived(t('chatPreview.unknownVar'));

  const verbLabel = $derived.by(() => {
    if (line.mode === 'announce') return line.color === 'primary' ? '/announce' : `/announce (${line.color})`;
    return line.verb ?? '';
  });
</script>

<div
  class="bb-chat__line"
  class:bb-chat__line--stack={line.mode !== 'chat' && line.mode !== 'me'}
  class:bb-chat__line--bare={line.mode === 'me'}
  style:--reply-delay="{index * 140}ms"
>
  <ChatBotName {seed} {active} />
  {#if line.mode === 'announce'}
    <div class="bb-chat__announce bb-chat__acc--{line.color ?? 'primary'}">
      <span class="bb-chat__head">
        <span class="bb-chat__via" title={t('chatPreview.runsVerb', { verb: line.verb ?? '' })}>Twitch {verbLabel}</span>
        {t('chatPreview.announcement')}
      </span>
      {#if line.segments.length}
        <span class="bb-chat__msg bb-chat__reply"><RehearsedSegments segments={line.segments} {unknownTitle} /></span>
      {:else}
        <span class="bb-chat__msg bb-chat__msg--empty">{t('chatPreview.addMessageAfter', { verb: line.verb ?? '' })}</span>
      {/if}
    </div>
  {:else if line.mode === 'shoutout'}
    <div class="bb-chat__shoutout">
      <span class="bb-chat__via" title={t('chatPreview.runsVerb', { verb: '/shoutout' })}>Twitch /shoutout</span>
      {#if line.target}
        <span class="bb-chat__msg bb-chat__reply">{t('chatPreview.shoutsOut')} <strong>@{line.target}</strong></span>
      {:else}
        <span class="bb-chat__msg bb-chat__msg--empty">{t('chatPreview.nameChannel')}</span>
      {/if}
    </div>
  {:else if line.mode === 'me'}
    <span class="bb-chat__via bb-chat__via--inline" title={t('chatPreview.runsVerb', { verb: '/me' })}>Twitch /me</span>
    {#if line.segments.length}
      <span class="bb-chat__msg bb-chat__reply bb-chat__msg--action"><RehearsedSegments segments={line.segments} {unknownTitle} /></span>
    {:else}
      <span class="bb-chat__msg bb-chat__msg--empty">{t('chatPreview.addActionAfterMe')}</span>
    {/if}
  {:else if line.mode === 'pin'}
    <div class="bb-chat__pin">
      <span class="bb-chat__head">
        <span class="bb-chat__via" title={t('chatPreview.runsVerb', { verb: '/pin' })}>Twitch /pin</span>
        {t('chatPreview.pinnedForStream')}
      </span>
      {#if line.segments.length}
        <span class="bb-chat__msg bb-chat__reply"><RehearsedSegments segments={line.segments} {unknownTitle} /></span>
      {:else}
        <span class="bb-chat__msg bb-chat__msg--empty">{t('chatPreview.addMessageAfter', { verb: '/pin' })}</span>
      {/if}
    </div>
  {:else}
    <span class="bb-chat__msg bb-chat__reply"><RehearsedSegments segments={line.segments} {unknownTitle} /></span>
  {/if}
</div>
