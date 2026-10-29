<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { page } from '$app/state';
  import { translate, type Locale } from '@bagel/kit/i18n';
  import {
    Bolota,
    rehearseCommand,
    rehearseReply,
    rehearseTimer,
    COMMAND_SAMPLES,
    normName,
    getI18n,
    type RehearsedLine,
    type Seg
  } from '@bagel/kit';
  import ChatPreview from '@bagel/ui/svelte/ChatPreview.svelte';

  const i18n = getI18n();

  let {
    name = '',
    response = '',
    args = '',
    kind = 'command',
    showViewer = true,
    viewerText = undefined as string | undefined,
    tag = undefined as string | undefined,
    broadcasterName = undefined as string | undefined,
    locale = undefined as Locale | undefined,
    samples = undefined as Record<string, string> | undefined,
    dynamic = true
  }: {
    name?: string;
    response?: string;
    args?: string;
    kind?: 'command' | 'reply' | 'timer';
    showViewer?: boolean;
    viewerText?: string;
    tag?: string;
    broadcasterName?: string;
    locale?: Locale;
    samples?: Record<string, string>;
    dynamic?: boolean;
  } = $props();
  const t = (key: string, params?: Record<string, string | number>) => translate(locale ?? i18n.locale, key, params);

  const viewerName = $derived(samples?.user ?? Object.entries(samples ?? {}).find(([key]) => key.endsWith(':user'))?.[1] ?? COMMAND_SAMPLES.user);

  const botSeed = $derived(broadcasterName ?? (page.data.displayName as string | undefined) ?? 'ItsBagelBot');

  const trigger = $derived('!' + (normName(name) || 'command') + (args ? ' ' + args : ''));

  const moduleFlags = $derived((page.data.moduleFlags as Record<string, boolean> | undefined) ?? {});

  const views = $derived.by<RehearsedLine[]>(() => {
    if (kind === 'command') return rehearseCommand(response, samples, moduleFlags);
    if (kind === 'timer') return rehearseTimer(response, moduleFlags);
    return rehearseReply(response, samples, { dynamic });
  });

  const effectiveShowViewer = $derived(kind === 'timer' ? false : showViewer);

  function verbLabelOf(v: RehearsedLine): string {
    if (v.mode === 'announce') {
      return v.color === 'primary' ? '/announce' : `/announce (${v.color})`;
    }
    return v.verb ?? '';
  }

  let typing = $state(false);
  let hovered = $state(false);
  let settle: ReturnType<typeof setTimeout> | undefined;
  let first = true;
  $effect(() => {
    void response;
    void name;
    if (first) {
      first = false;
      return;
    }
    typing = true;
    clearTimeout(settle);
    settle = setTimeout(() => (typing = false), 550);
    return () => clearTimeout(settle);
  });
</script>

{#snippet botName()}
  <span class="bb-chat__name bb-chat__name--bot">
    <span class="bb-chat__avatar" aria-hidden="true"><Bolota name={botSeed} size={20} active={typing || hovered} /></span>
    ItsBagelBot
  </span>
{/snippet}

{#snippet segs(list: Seg[])}
  {#each list as seg, i (i)}
    {#if seg.kind === 'sample'}<mark>{seg.text}</mark>
    {:else if seg.kind === 'unknown'}<mark class="bb-chat__unknown" title={t('chatPreview.unknownVar')}>{seg.text}</mark>
    {:else}{seg.text}{/if}
  {/each}
{/snippet}

<ChatPreview
  title={tag ?? t('chatPreview.rehearsal')}
  label={t('chatPreview.ariaPreview')}
  class="chat-preview"
  onpointerenter={() => (hovered = true)}
  onpointerleave={() => (hovered = false)}
>
  {#if effectiveShowViewer}
    <div class="bb-chat__line">
      <span class="bb-chat__name bb-chat__name--viewer">{viewerName}</span>
      <span class="bb-chat__msg" class:bb-chat__msg--cmd={viewerText === undefined}>{viewerText ?? trigger}</span>
    </div>
  {/if}
  {#if typing}
    <div class="bb-chat__line">
      {@render botName()}
      <span class="bb-chat__msg bb-chat__msg--typing" aria-label={t('chatPreview.ariaTyping')}>
        <span class="bb-drawline" aria-hidden="true"></span>
      </span>
    </div>
  {:else if views.length === 0}
    <div class="bb-chat__line">
      {@render botName()}
      <span class="bb-chat__msg bb-chat__msg--empty">{t('chatPreview.nothingToSay')}</span>
    </div>
  {:else}
    {#each views as v, li (li)}
      <div
        class="bb-chat__line"
        class:bb-chat__line--stack={v.mode !== 'chat' && v.mode !== 'me'}
        class:bb-chat__line--bare={v.mode === 'me'}
        style:--reply-delay="{li * 140}ms"
      >
        {@render botName()}
        {#if v.mode === 'announce'}
          <div class="bb-chat__announce bb-chat__acc--{v.color ?? 'primary'}">
            <span class="bb-chat__head">
              <span class="bb-chat__via" title={t('chatPreview.runsVerb', { verb: v.verb ?? '' })}>Twitch {verbLabelOf(v)}</span>
              {t('chatPreview.announcement')}
            </span>
            {#if v.segments.length}
              <span class="bb-chat__msg bb-chat__reply">{@render segs(v.segments)}</span>
            {:else}
              <span class="bb-chat__msg bb-chat__msg--empty">{t('chatPreview.addMessageAfter', { verb: v.verb ?? '' })}</span>
            {/if}
          </div>
        {:else if v.mode === 'shoutout'}
          <div class="bb-chat__shoutout">
            <span class="bb-chat__via" title={t('chatPreview.runsVerb', { verb: '/shoutout' })}>Twitch /shoutout</span>
            {#if v.target}
              <span class="bb-chat__msg bb-chat__reply">{t('chatPreview.shoutsOut')} <strong>@{v.target}</strong></span>
            {:else}
              <span class="bb-chat__msg bb-chat__msg--empty">{t('chatPreview.nameChannel')}</span>
            {/if}
          </div>
        {:else if v.mode === 'me'}
          <span class="bb-chat__via bb-chat__via--inline" title={t('chatPreview.runsVerb', { verb: '/me' })}>Twitch /me</span>
          {#if v.segments.length}
            <span class="bb-chat__msg bb-chat__reply bb-chat__msg--action">{@render segs(v.segments)}</span>
          {:else}
            <span class="bb-chat__msg bb-chat__msg--empty">{t('chatPreview.addActionAfterMe')}</span>
          {/if}
        {:else if v.mode === 'pin'}
          <div class="bb-chat__pin">
            <span class="bb-chat__head">
              <span class="bb-chat__via" title={t('chatPreview.runsVerb', { verb: '/pin' })}>Twitch /pin</span>
              {t('chatPreview.pinnedForStream')}
            </span>
            {#if v.segments.length}
              <span class="bb-chat__msg bb-chat__reply">{@render segs(v.segments)}</span>
            {:else}
              <span class="bb-chat__msg bb-chat__msg--empty">{t('chatPreview.addMessageAfter', { verb: '/pin' })}</span>
            {/if}
          </div>
        {:else}
          <span class="bb-chat__msg bb-chat__reply">{@render segs(v.segments)}</span>
        {/if}
      </div>
    {/each}
  {/if}
</ChatPreview>

<style>
  :global(.chat-preview) { margin-top: 10px; }
</style>
