<script lang="ts">
  // Live chat rehearsal: acts out a response as it will look in Twitch chat —
  // a viewer types the trigger, the bot "types" for a beat, then replies with
  // sample values substituted into the tokens. Re-runs (debounced) as the
  // response is edited, so authors see the real thing, not a template string.
  //
  // This component only RENDERS. What the bot would actually send — which
  // tokens expand, whether a leading /announce, /shoutout, /pin or /me becomes
  // a native Twitch action, how many messages a multi-line response mints —
  // is computed by the shared rehearsal core (@bagel/shared rehearsal.ts),
  // which mirrors the Go engine line by line. `kind` picks the surface:
  //
  //   kind="command" — custom "!command" responses: full command tokens,
  //   slash-verb routing per line, up to 5 messages (one per line).
  //
  //   kind="reply" — module replies (alerts, triggers, rewards, built-ins,
  //   gateway commands): ONLY the tokens in `samples` (plus the dynamic set
  //   unless dynamic={false}), one plain message, no slash-verb actions —
  //   those surfaces never run Translate, so "/announce hi" is sent, and
  //   shown, as literal text.
  import {
    rehearseCommand,
    rehearseReply,
    COMMAND_SAMPLES,
    normName,
    getI18n,
    type RehearsedLine,
    type Seg
  } from '@bagel/shared';

  const { t } = getI18n();

  let {
    name = '',
    response = '',
    args = '',
    kind = 'command',
    showViewer = true,
    viewerText = undefined as string | undefined,
    tag = undefined as string | undefined,
    samples = undefined as Record<string, string> | undefined,
    dynamic = true
  }: {
    name?: string;
    response?: string;
    args?: string;
    // Which bot expansion path this surface rehearses (see header comment).
    kind?: 'command' | 'reply';
    showViewer?: boolean;
    // viewerText renders the viewer line verbatim (a plain chat message, no "!"
    // trigger) — used by trigger-word rehearsals where a normal message fires the
    // reply. When unset the viewer types the "!command" trigger.
    viewerText?: string;
    tag?: string;
    // kind="command": overrides merged over the standard command samples.
    // kind="reply": the surface's OWN token map — nothing else substitutes.
    samples?: Record<string, string>;
    // kind="reply" only: false for surfaces whose bot path is a bare string
    // replacer with no {random}/{choice:…} fallback (govee, clip).
    dynamic?: boolean;
  } = $props();

  // The sample viewer typing the trigger; reply surfaces may carry no {user}.
  const viewerName = $derived(samples?.user ?? COMMAND_SAMPLES.user);

  const trigger = $derived('!' + (normName(name) || 'command') + (args ? ' ' + args : ''));

  // Twitch announcement accent colors. "primary" is the channel accent.
  const ACCENT: Record<string, string> = {
    primary: 'var(--bb-tan-light)',
    blue: '#4a9eff',
    green: '#52b788',
    orange: '#ff9f45',
    purple: '#c77dff'
  };

  const views = $derived.by<RehearsedLine[]>(() =>
    kind === 'command' ? rehearseCommand(response, samples) : rehearseReply(response, samples, { dynamic })
  );

  // Human label for the "uses Twitch command" badge.
  function verbLabelOf(v: RehearsedLine): string {
    if (v.mode === 'announce') {
      return v.color === 'primary' ? '/announce' : `/announce (${v.color})`;
    }
    return v.verb ?? '';
  }

  // Typing beat: edits flip to the dots, settle back to the reply. Debounced so
  // the bot doesn't stutter on every keystroke.
  let typing = $state(false);
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

{#snippet segs(list: Seg[])}
  {#each list as seg, i (i)}
    {#if seg.kind === 'sample'}<mark>{seg.text}</mark>
    {:else if seg.kind === 'unknown'}<mark class="unknown" title={t('chatPreview.unknownVar')}>{seg.text}</mark>
    {:else}{seg.text}{/if}
  {/each}
{/snippet}

<div class="chat" aria-label={t('chatPreview.ariaPreview')}>
  <span class="chat-tag">{tag ?? t('chatPreview.rehearsal')}</span>
  {#if showViewer}
    <div class="line viewer">
      <span class="who viewer-name">{viewerName}</span>
      <span class="msg" class:plain={viewerText !== undefined}>{viewerText ?? trigger}</span>
    </div>
  {/if}
  {#if typing}
    <div class="line bot">
      <span class="who bot-name">
        <img src="/logo.png" alt="" class="bot-avatar" />
        ItsBagelBot
      </span>
      <span class="msg typing" aria-label={t('chatPreview.ariaTyping')}>
        <span class="tdot"></span><span class="tdot"></span><span class="tdot"></span>
      </span>
    </div>
  {:else if views.length === 0}
    <div class="line bot">
      <span class="who bot-name">
        <img src="/logo.png" alt="" class="bot-avatar" />
        ItsBagelBot
      </span>
      <span class="msg empty">{t('chatPreview.nothingToSay')}</span>
    </div>
  {:else}
    <!-- One bot message per rehearsed line, staggered like the worker's send order. -->
    {#each views as v, li (li)}
      <div
        class="line bot"
        class:special={v.mode !== 'chat'}
        class:me={v.mode === 'me'}
        style="--reply-delay: {li * 140}ms"
      >
        <span class="who bot-name">
          <img src="/logo.png" alt="" class="bot-avatar" />
          ItsBagelBot
        </span>
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
    border-radius: 8px 8px;
    background: rgba(0, 0, 0, 0.3);
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .chat-tag {
    position: absolute;
    top: -8px;
    left: 10px;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 10px;
    letter-spacing: 0.02em;
    color: var(--bb-green-glow);
    background: var(--bb-bg-0, #0a0a0a);
    padding: 0 6px;
  }

  .line { display: flex; align-items: baseline; gap: 8px; min-width: 0; }
  /* Slash-verb actions render a block (announcement box / shoutout card), so the
     bot line stacks its name above the action instead of sharing a baseline. */
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
  .viewer-name::after, .bot-name::after { content: ':'; color: var(--bb-muted); font-weight: 400; }
  /* /me actions carry no colon (Twitch renders them as "name action…"). */
  .line.bot.me .bot-name::after { content: none; }
  .line.bot.special .bot-name::after { content: none; }
  .bot-name { color: var(--bb-green-glow); }
  .bot-avatar { width: 14px; height: 14px; border-radius: 8px; }

  .msg {
    font-family: var(--bb-font-body);
    font-size: 13px;
    line-height: 1.5;
    color: var(--bb-white);
    overflow-wrap: anywhere;
    min-width: 0;
  }
  .line.viewer .msg { font-family: var(--bb-font-mono); color: var(--bb-tan-light); font-size: 12.5px; }
  /* A plain viewer message (trigger-word rehearsal) reads like normal chat. */
  .line.viewer .msg.plain { font-family: var(--bb-font-body); color: var(--bb-white); font-size: 13px; }

  .reply { animation: reply-in 240ms var(--bb-ease-out-back, ease-out) both; animation-delay: var(--reply-delay, 0ms); }
  @keyframes reply-in {
    from { opacity: 0; transform: translateY(4px); }
    to { opacity: 1; transform: translateY(0); }
  }

  .msg mark {
    background: rgba(82, 183, 136, 0.14);
    color: var(--bb-green-glow, #52b788);
    border-radius: 8px;
    padding: 0 3px;
  }
  .msg mark.unknown {
    background: rgba(176, 90, 70, 0.16);
    color: #cf8a78;
    font-family: var(--bb-font-mono);
    font-size: 12px;
  }
  .msg.empty { color: var(--bb-muted); font-style: italic; }

  /* "uses Twitch command" badge: names the verb the response fires. */
  .via {
    font-family: var(--bb-font-mono);
    font-weight: 600;
    font-size: 10px;
    letter-spacing: 0.02em;
    color: var(--acc, var(--bb-green-glow));
    background: color-mix(in srgb, var(--acc, var(--bb-green-glow)) 14%, transparent);
    border: 1px solid color-mix(in srgb, var(--acc, var(--bb-green-glow)) 40%, transparent);
    border-radius: 999px;
    padding: 1px 7px;
    white-space: nowrap;
  }
  .via.inline { margin-right: 2px; }

  /* Twitch announcement: a highlighted callout with the announce color accent. */
  .announce {
    width: 100%;
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    gap: 5px;
    padding: 9px 11px;
    border-radius: 8px 8px;
    background: color-mix(in srgb, var(--acc) 10%, rgba(0, 0, 0, 0.25));
    animation: reply-in 240ms var(--bb-ease-out-back, ease-out) both;
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

  /* Twitch shoutout: a compact card — the native /shoutout is an action, not a
     chat line, so it reads as "shouts out @channel". */
  .shoutout {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    padding: 7px 11px;
    border-radius: 8px 8px;
    border: 1px dashed rgba(82, 183, 136, 0.4);
    background: rgba(82, 183, 136, 0.06);
    animation: reply-in 240ms var(--bb-ease-out-back, ease-out) both;
    animation-delay: var(--reply-delay, 0ms);
  }
  .shoutout .reply strong { color: var(--bb-green-glow); }

  /* /pin sends a real bot message and anchors it for the current stream. */
  .pin {
    width: 100%;
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 8px 10px;
    border-radius: 8px;
    border: 1px solid rgba(199, 125, 255, 0.35);
    background: rgba(199, 125, 255, 0.07);
    animation: reply-in 240ms var(--bb-ease-out-back, ease-out) both;
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

  /* /me action: italic, tinted like the bot's name (no colon before it). */
  .msg.action { font-style: italic; color: var(--bb-green-glow); }

  .typing { display: inline-flex; gap: 4px; align-items: center; padding: 4px 0; }
  .tdot {
    width: 5px; height: 5px; border-radius: 50%;
    background: var(--bb-muted);
    animation: tbounce 900ms ease-in-out infinite;
  }
  .tdot:nth-child(2) { animation-delay: 150ms; }
  .tdot:nth-child(3) { animation-delay: 300ms; }
  @keyframes tbounce {
    0%, 60%, 100% { transform: translateY(0); opacity: 0.5; }
    30% { transform: translateY(-3px); opacity: 1; }
  }

  @media (prefers-reduced-motion: reduce) {
    .reply, .announce, .shoutout, .pin { animation: none; }
    .tdot { animation: none; }
  }
</style>
