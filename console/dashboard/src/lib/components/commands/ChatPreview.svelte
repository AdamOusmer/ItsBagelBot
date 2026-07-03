<script lang="ts">
  // Live chat rehearsal: acts out the command as it will look in Twitch chat —
  // a viewer types the trigger, the bot "types" for a beat, then replies with
  // sample values substituted into the tokens. Re-runs (debounced) as the
  // response is edited, so authors see the real thing, not a template string.
  //
  // When the response leads with a Twitch slash-verb (/announce…, /shoutout,
  // /me) the worker turns it into that native action instead of a plain chat
  // line (see app/worker/module/slash.go). The rehearsal mirrors that parse so
  // authors can see they're driving a Twitch command — an announcement callout,
  // a shoutout card, or an italic /me action — with a badge naming the verb.
  import { normName, getI18n } from '@bagel/shared';

  const { t } = getI18n();

  let { name = '', response = '' }: { name?: string; response?: string } = $props();

  const SAMPLES: Record<string, string> = {
    user: 'sesame_sam',
    target: '@ferret_king',
    uptime: '3h 24m',
    followage: '8 months',
    raider: 'CrustyCrumbs',
    raider_login: 'crustycrumbs',
    viewers: '42'
  };

  const trigger = $derived('!' + (normName(name) || 'command'));

  // --- Slash-verb parse, mirrored from app/worker/module/slash.go ---------
  // The worker recognizes a leading verb, rewrites the outgress Type, and
  // strips the verb from the text. We reproduce the same matching (whole-verb
  // or "verb " prefix) so the rehearsal renders the same action the bot sends.
  type Mode = 'chat' | 'announce' | 'shoutout' | 'me';
  type Parsed = {
    mode: Mode;
    body: string; // text with the verb stripped (what actually gets shown)
    verb?: string; // the matched verb, e.g. "/announcegreen"
    color?: string; // announce color key
    target?: string; // shoutout target (leading @ removed)
  };

  // Twitch announcement accent colors. "primary" is the channel accent.
  const ACCENT: Record<string, string> = {
    primary: '#9147ff',
    blue: '#4a9eff',
    green: '#52b788',
    orange: '#ff9f45',
    purple: '#c77dff'
  };

  // Longest verbs first so "/announceblue" isn't mistaken for "/announce".
  const ANNOUNCE: [string, string][] = [
    ['/announceblue', 'blue'],
    ['/announcegreen', 'green'],
    ['/announceorange', 'orange'],
    ['/announcepurple', 'purple'],
    ['/announce', 'primary']
  ];

  // cutVerb: match verb as the whole string or as a "verb " prefix; returns the
  // remainder with the single separating space removed (null when no match).
  function cutVerb(text: string, verb: string): string | null {
    if (text === verb) return '';
    if (text.startsWith(verb + ' ')) return text.slice(verb.length + 1);
    return null;
  }

  const parsed = $derived.by<Parsed>(() => {
    const text = response;
    for (const [verb, color] of ANNOUNCE) {
      const rest = cutVerb(text, verb);
      if (rest !== null) return { mode: 'announce', body: rest, verb, color };
    }
    const so = cutVerb(text, '/shoutout');
    if (so !== null) {
      const trimmed = so.replace(/^ +/, '');
      const sp = trimmed.indexOf(' ');
      const rawTarget = sp === -1 ? trimmed : trimmed.slice(0, sp);
      const remainder = sp === -1 ? '' : trimmed.slice(sp + 1).replace(/^ +/, '');
      return { mode: 'shoutout', body: remainder, verb: '/shoutout', target: rawTarget.replace(/^@/, '') };
    }
    // /me is a plain passthrough on the wire (verb kept in Text), but Twitch
    // renders it as an italic action — strip the verb for display.
    const me = cutVerb(text, '/me');
    if (me !== null) return { mode: 'me', body: me, verb: '/me' };
    return { mode: 'chat', body: text };
  });

  // Human label for the "uses Twitch command" badge.
  const verbLabel = $derived.by(() => {
    if (parsed.mode === 'announce') {
      return parsed.color === 'primary' ? '/announce' : `/announce (${parsed.color})`;
    }
    return parsed.verb ?? '';
  });

  // Substituted segments over the *stripped* body: known tokens become
  // highlighted sample values, unknown {tokens} stay marked so typos are visible.
  type Seg = { text: string; kind: 'plain' | 'sample' | 'unknown' };
  const segments = $derived.by<Seg[]>(() => {
    const body = parsed.body;
    const out: Seg[] = [];
    const re = /\{([a-z_]+)\}/gi;
    let last = 0;
    for (const m of body.matchAll(re)) {
      if (m.index > last) out.push({ text: body.slice(last, m.index), kind: 'plain' });
      const key = m[1].toLowerCase();
      if (key in SAMPLES) out.push({ text: SAMPLES[key], kind: 'sample' });
      else out.push({ text: m[0], kind: 'unknown' });
      last = m.index + m[0].length;
    }
    if (last < body.length) out.push({ text: body.slice(last), kind: 'plain' });
    return out;
  });

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

<div class="chat" aria-label={t('chatPreview.ariaPreview')}>
  <span class="chat-tag">{t('chatPreview.rehearsal')}</span>
  <div class="line viewer">
    <span class="who viewer-name">sesame_sam</span>
    <span class="msg">{trigger}</span>
  </div>
  <div class="line bot" class:special={parsed.mode !== 'chat'} class:me={parsed.mode === 'me'}>
    <span class="who bot-name">
      <img src="/logo.png" alt="" class="bot-avatar" />
      ItsBagelBot
    </span>
    {#if typing}
      <span class="msg typing" aria-label={t('chatPreview.ariaTyping')}>
        <span class="tdot"></span><span class="tdot"></span><span class="tdot"></span>
      </span>
    {:else if parsed.mode === 'announce'}
      <div class="announce" style="--acc: {ACCENT[parsed.color ?? 'primary']}">
        <span class="announce-head">
          <span class="via" title={t('chatPreview.runsVerb', { verb: parsed.verb ?? '' })}>Twitch {verbLabel}</span>
          {t('chatPreview.announcement')}
        </span>
        {#if segments.length}
          <span class="msg reply">
            {#each segments as seg, i (i)}
              {#if seg.kind === 'sample'}<mark>{seg.text}</mark>
              {:else if seg.kind === 'unknown'}<mark class="unknown" title={t('chatPreview.unknownVar')}>{seg.text}</mark>
              {:else}{seg.text}{/if}
            {/each}
          </span>
        {:else}
          <span class="msg empty">{t('chatPreview.addMessageAfter', { verb: parsed.verb ?? '' })}</span>
        {/if}
      </div>
    {:else if parsed.mode === 'shoutout'}
      <div class="shoutout">
        <span class="via" title={t('chatPreview.runsVerb', { verb: '/shoutout' })}>Twitch /shoutout</span>
        {#if parsed.target}
          <span class="msg reply">{t('chatPreview.shoutsOut')} <strong>@{parsed.target}</strong></span>
        {:else}
          <span class="msg empty">{t('chatPreview.nameChannel')}</span>
        {/if}
      </div>
    {:else if parsed.mode === 'me'}
      <span class="via inline" title={t('chatPreview.runsVerb', { verb: '/me' })}>Twitch /me</span>
      {#if segments.length}
        <span class="msg reply action">
          {#each segments as seg, i (i)}
            {#if seg.kind === 'sample'}<mark>{seg.text}</mark>
            {:else if seg.kind === 'unknown'}<mark class="unknown" title={t('chatPreview.unknownVar')}>{seg.text}</mark>
            {:else}{seg.text}{/if}
          {/each}
        </span>
      {:else}
        <span class="msg empty">{t('chatPreview.addActionAfterMe')}</span>
      {/if}
    {:else if parsed.body.trim()}
      <span class="msg reply">
        {#each segments as seg, i (i)}
          {#if seg.kind === 'sample'}<mark>{seg.text}</mark>
          {:else if seg.kind === 'unknown'}<mark class="unknown" title={t('chatPreview.unknownVar')}>{seg.text}</mark>
          {:else}{seg.text}{/if}
        {/each}
      </span>
    {:else}
      <span class="msg empty">{t('chatPreview.nothingToSay')}</span>
    {/if}
  </div>
</div>

<style>
  .chat {
    position: relative;
    margin-top: 10px;
    padding: 14px 14px 12px;
    border: 1px solid var(--rule, rgba(240, 236, 228, 0.1));
    border-left: 2px solid rgba(82, 183, 136, 0.5);
    border-radius: var(--bb-radius-sm, 2px);
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
  .bot-avatar { width: 14px; height: 14px; border-radius: 2px; }

  .msg {
    font-family: var(--bb-font-body);
    font-size: 13px;
    line-height: 1.5;
    color: var(--bb-white);
    overflow-wrap: anywhere;
    min-width: 0;
  }
  .line.viewer .msg { font-family: var(--bb-font-mono); color: var(--bb-tan-light); font-size: 12.5px; }

  .reply { animation: reply-in 240ms var(--bb-ease-out-back, ease-out) both; }
  @keyframes reply-in {
    from { opacity: 0; transform: translateY(4px); }
    to { opacity: 1; transform: translateY(0); }
  }

  .msg mark {
    background: rgba(82, 183, 136, 0.14);
    color: var(--bb-green-glow, #52b788);
    border-radius: 2px;
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
    border-radius: var(--bb-radius-sm, 4px);
    border-left: 3px solid var(--acc);
    background: color-mix(in srgb, var(--acc) 10%, rgba(0, 0, 0, 0.25));
    animation: reply-in 240ms var(--bb-ease-out-back, ease-out) both;
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
    border-radius: var(--bb-radius-sm, 4px);
    border: 1px dashed rgba(82, 183, 136, 0.4);
    background: rgba(82, 183, 136, 0.06);
    animation: reply-in 240ms var(--bb-ease-out-back, ease-out) both;
  }
  .shoutout .reply strong { color: var(--bb-green-glow); }

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
    .reply, .announce, .shoutout { animation: none; }
    .tdot { animation: none; }
  }
</style>
