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

  const viewerName = $derived(samples?.user ?? COMMAND_SAMPLES.user);

  const botSeed = $derived(broadcasterName ?? (page.data.displayName as string | undefined) ?? 'ItsBagelBot');

  const trigger = $derived('!' + (normName(name) || 'command') + (args ? ' ' + args : ''));

  const ACCENT: Record<string, string> = {
    primary: 'var(--bb-tan-light)',
    blue: '#4a9eff',
    green: '#52b788',
    orange: '#ff9f45',
    purple: '#c77dff'
  };

  const views = $derived.by<RehearsedLine[]>(() => {
    if (kind === 'command') return rehearseCommand(response, samples);
    if (kind === 'timer') return rehearseTimer(response);
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
  <span class="who bot-name">
    <span class="avatar" aria-hidden="true"><Bolota name={botSeed} size={20} active={typing || hovered} /></span>
    ItsBagelBot
  </span>
{/snippet}

{#snippet segs(list: Seg[])}
  {#each list as seg, i (i)}
    {#if seg.kind === 'sample'}<mark>{seg.text}</mark>
    {:else if seg.kind === 'unknown'}<mark class="unknown" title={t('chatPreview.unknownVar')}>{seg.text}</mark>
    {:else}{seg.text}{/if}
  {/each}
{/snippet}

<div
  class="chat"
  role="group"
  aria-label={t('chatPreview.ariaPreview')}
  onpointerenter={() => (hovered = true)}
  onpointerleave={() => (hovered = false)}
>
  <span class="bb-tag bb-tag--bare chat-tag">{tag ?? t('chatPreview.rehearsal')}</span>
  {#if effectiveShowViewer}
    <div class="line viewer">
      <span class="who viewer-name">{viewerName}</span>
      <span class="msg" class:plain={viewerText !== undefined}>{viewerText ?? trigger}</span>
    </div>
  {/if}
  {#if typing}
    <div class="line bot">
      {@render botName()}
      <span class="msg typing" aria-label={t('chatPreview.ariaTyping')}>
        <span class="bb-drawline" aria-hidden="true"></span>
      </span>
    </div>
  {:else if views.length === 0}
    <div class="line bot">
      {@render botName()}
      <span class="msg empty">{t('chatPreview.nothingToSay')}</span>
    </div>
  {:else}
    {#each views as v, li (li)}
      <div
        class="line bot"
        class:special={v.mode !== 'chat'}
        class:me={v.mode === 'me'}
        style="--reply-delay: {li * 140}ms"
      >
        {@render botName()}
        {#if v.mode === 'announce'}
          <div class="announce" style="--acc: {ACCENT[v.color ?? 'primary']}">
            <span class="announce-head">
              <span class="via" title={t('chatPreview.runsVerb', { verb: v.verb ?? '' })}>Twitch {verbLabelOf(v)}</span>
              {t('chatPreview.announcement')}
            </span>
            {#if v.segments.length}
              <span class="msg reply">{@render segs(v.segments)}</span>
            {:else}
              <span class="msg empty">{t('chatPreview.addMessageAfter', { verb: v.verb ?? '' })}</span>
            {/if}
          </div>
        {:else if v.mode === 'shoutout'}
          <div class="shoutout">
            <span class="via" title={t('chatPreview.runsVerb', { verb: '/shoutout' })}>Twitch /shoutout</span>
            {#if v.target}
              <span class="msg reply">{t('chatPreview.shoutsOut')} <strong>@{v.target}</strong></span>
            {:else}
              <span class="msg empty">{t('chatPreview.nameChannel')}</span>
            {/if}
          </div>
        {:else if v.mode === 'me'}
          <span class="via inline" title={t('chatPreview.runsVerb', { verb: '/me' })}>Twitch /me</span>
          {#if v.segments.length}
            <span class="msg reply action">{@render segs(v.segments)}</span>
          {:else}
            <span class="msg empty">{t('chatPreview.addActionAfterMe')}</span>
          {/if}
        {:else if v.mode === 'pin'}
          <div class="pin">
            <span class="pin-head">
              <span class="via" title={t('chatPreview.runsVerb', { verb: '/pin' })}>Twitch /pin</span>
              {t('chatPreview.pinnedForStream')}
            </span>
            {#if v.segments.length}
              <span class="msg reply">{@render segs(v.segments)}</span>
            {:else}
              <span class="msg empty">{t('chatPreview.addMessageAfter', { verb: '/pin' })}</span>
            {/if}
          </div>
        {:else}
          <span class="msg reply">{@render segs(v.segments)}</span>
        {/if}
      </div>
    {/each}
  {/if}
</div>

<style>
  .chat {
    position: relative;
    margin-top: 10px;
    padding: 14px 14px 12px;
    border: 1px solid var(--rule, rgba(240, 236, 228, 0.1));
    border-radius: var(--bb-radius-md);
    background: rgba(0, 0, 0, 0.3);
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .chat-tag {
    position: absolute;
    top: -8px;
    left: 10px;
    font-size: 10px;
    color: var(--bb-green-glow);
    background: var(--bb-bg-0, #0a0a0a);
    padding: 0 6px;
  }

  .line { display: flex; align-items: baseline; gap: 8px; min-width: 0; }
  .line.bot.special { flex-direction: column; align-items: flex-start; gap: 6px; }
  .who {
    font-family: var(--bb-font-body);
    font-weight: 700;
    font-size: 12.5px;
    flex: none;
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  .viewer-name { color: var(--bb-tan-light); }
  .avatar {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: 20px;
    height: 20px;
    border-radius: 50%;
    overflow: hidden;
  }
  .viewer-name::after, .bot-name::after { content: ':'; color: var(--bb-muted); font-weight: 400; }
  .line.bot.me .bot-name::after { content: none; }
  .line.bot.special .bot-name::after { content: none; }
  .bot-name { color: var(--bb-green-glow); }

  .msg {
    font-family: var(--bb-font-body);
    font-size: 13px;
    line-height: 1.5;
    color: var(--bb-white);
    overflow-wrap: anywhere;
    min-width: 0;
  }
  .line.viewer .msg { font-family: var(--bb-font-mono); color: var(--bb-tan-light); font-size: 12.5px; }
  .line.viewer .msg.plain { font-family: var(--bb-font-body); color: var(--bb-white); font-size: 13px; }

  .reply { animation: reply-in 320ms var(--bb-ease-out-expo, ease-out) both; animation-delay: var(--reply-delay, 0ms); }
  @keyframes reply-in {
    from { opacity: 0; transform: translateX(-8px); }
    to { opacity: 1; transform: none; }
  }

  .msg mark {
    background: rgba(82, 183, 136, 0.14);
    color: var(--bb-green-glow, #52b788);
    border-radius: var(--bb-radius-xs);
    padding: 0 3px;
  }
  .msg mark.unknown {
    background: rgba(176, 90, 70, 0.16);
    color: #cf8a78;
    font-family: var(--bb-font-mono);
    font-size: 12px;
  }
  .msg.empty { color: var(--bb-muted); font-style: italic; }

  .via {
    font-family: var(--bb-font-mono);
    font-weight: 600;
    font-size: 10px;
    letter-spacing: 0.02em;
    color: var(--acc, var(--bb-green-glow));
    background: color-mix(in srgb, var(--acc, var(--bb-green-glow)) 14%, transparent);
    border: 1px solid color-mix(in srgb, var(--acc, var(--bb-green-glow)) 40%, transparent);
    border-radius: var(--bb-radius-pill);
    padding: 1px 7px;
    white-space: nowrap;
  }
  .via.inline { margin-right: 2px; }

  .announce {
    width: 100%;
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    gap: 5px;
    padding: 9px 11px;
    border-radius: var(--bb-radius-md);
    background: color-mix(in srgb, var(--acc) 10%, rgba(0, 0, 0, 0.25));
    animation: reply-in 320ms var(--bb-ease-out-expo, ease-out) both;
    animation-delay: var(--reply-delay, 0ms);
  }
  .announce-head {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 10.5px;
    letter-spacing: 0.03em;
    text-transform: uppercase;
    color: var(--acc);
  }

  .shoutout {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    padding: 7px 11px;
    border-radius: var(--bb-radius-md);
    border: 1px dashed rgba(82, 183, 136, 0.4);
    background: rgba(82, 183, 136, 0.06);
    animation: reply-in 320ms var(--bb-ease-out-expo, ease-out) both;
    animation-delay: var(--reply-delay, 0ms);
  }
  .shoutout .reply strong { color: var(--bb-green-glow); }

  .pin {
    width: 100%;
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 8px 10px;
    border-radius: var(--bb-radius-sm);
    border: 1px solid rgba(199, 125, 255, 0.35);
    background: rgba(199, 125, 255, 0.07);
    animation: reply-in 320ms var(--bb-ease-out-expo, ease-out) both;
    animation-delay: var(--reply-delay, 0ms);
  }
  .pin-head {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--bb-muted);
    font-family: var(--bb-font-display);
    font-size: 10.5px;
  }

  .msg.action { font-style: italic; color: var(--bb-green-glow); }

  .typing { display: inline-flex; align-items: center; padding: 4px 0; }

  @media (prefers-reduced-motion: reduce) {
    .reply, .announce, .shoutout, .pin { animation: none; }
  }
</style>
