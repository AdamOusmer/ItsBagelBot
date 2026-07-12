<script lang="ts">
  // Live chat rehearsal: acts out the command as it will look in Twitch chat —
  // a viewer types the trigger, the bot "types" for a beat, then replies with
  // sample values substituted into the tokens. Re-runs (debounced) as the
  // response is edited, so authors see the real thing, not a template string.
  //
  // When the response leads with a Twitch slash-verb (/announce…, /shoutout, /pin,
  // /me) the worker turns it into that native action instead of a plain chat
  // line (see app/sesame/engine/slash.go). The rehearsal mirrors that parse so
  // authors can see they're driving a Twitch command — an announcement callout,
  // a shoutout card, a stream-long pin, or an italic /me action — with a badge
  // naming the verb.
  // A multi-line response (newline-delimited, up to 5 lines) is acted out as
  // the bot sending one chat message per line, in order — the same fan-out the
  // worker performs — so authors see exactly how many messages they're minting.
  import { normName, responseLines, RESPONSE_MAX_LINES, getI18n } from '@bagel/shared';

  const { t } = getI18n();

  // Reused beyond custom commands: built-in commands pass `args` (the text a
  // viewer types after the trigger, e.g. "!clip That is amazing") and sample
  // overrides; event-driven module replies pass showViewer=false + a `tag` (the
  // firing event, e.g. "on follow") so only the bot line renders.
  let {
    name = '',
    response = '',
    args = '',
    showViewer = true,
    viewerText = undefined as string | undefined,
    tag = undefined as string | undefined,
    samples = undefined as Record<string, string> | undefined,
    samplesOnly = false
  }: {
    name?: string;
    response?: string;
    args?: string;
    showViewer?: boolean;
    // viewerText renders the viewer line verbatim (a plain chat message, no "!"
    // trigger) — used by trigger-word rehearsals where a normal message fires the
    // reply. When unset the viewer types the "!command" trigger.
    viewerText?: string;
    tag?: string;
    samples?: Record<string, string>;
    // samplesOnly drops the default command samples entirely: gateway module
    // replies substitute ONLY their own tokens ({player}, {wins}, ...) — the
    // bot never expands {user}/{uptime} there, so previewing those as values
    // would rehearse a reply the bot cannot produce. Unknown tokens stay
    // marked, exactly like a typo in a custom command.
    samplesOnly?: boolean;
  } = $props();

  const DEFAULT_SAMPLES: Record<string, string> = {
    user: 'sesame_sam',
    args: 'aaaa',
    target: 'ferret_king',
    uptime: '3h 24m',
    followage: '8 months',
    tier: '1000',
    bits: '500',
    raider: 'CrustyCrumbs',
    raider_login: 'crustycrumbs',
    viewers: '42'
  };
  // Caller overrides win (e.g. a raid alert where {user} is the raiding channel).
  const SAMPLES = $derived<Record<string, string>>(
    samplesOnly ? (samples ?? {}) : { ...DEFAULT_SAMPLES, ...(samples ?? {}) }
  );
  // The sample viewer typing the trigger; module replies carry no {user} sample.
  const viewerName = $derived(SAMPLES.user ?? DEFAULT_SAMPLES.user);

  const trigger = $derived('!' + (normName(name) || 'command') + (args ? ' ' + args : ''));

  // --- Slash-verb parse, mirrored from app/worker/module/slash.go ---------
  // Sesame recognizes a leading verb, rewrites the outgress Type, and
  // strips the verb from the text. We reproduce the same matching (whole-verb
  // or "verb " prefix) so the rehearsal renders the same action the bot sends.
  type Mode = 'chat' | 'announce' | 'shoutout' | 'pin' | 'me';
  type Parsed = {
    mode: Mode;
    body: string; // text with the verb stripped (what actually gets shown)
    verb?: string; // the matched verb, e.g. "/announcegreen"
    color?: string; // announce color key
    target?: string; // shoutout target (leading @ removed)
  };

  // Twitch announcement accent colors. "primary" is the channel accent.
  const ACCENT: Record<string, string> = {
    primary: 'var(--bb-tan-light)',
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

  function parseLine(text: string): Parsed {
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
    const pin = cutVerb(text, '/pin');
    if (pin !== null) return { mode: 'pin', body: pin, verb: '/pin' };
    // /me is a plain passthrough on the wire (verb kept in Text), but Twitch
    // renders it as an italic action — strip the verb for display.
    const me = cutVerb(text, '/me');
    if (me !== null) return { mode: 'me', body: me, verb: '/me' };
    return { mode: 'chat', body: text };
  }

  // Human label for the "uses Twitch command" badge.
  function verbLabelOf(p: Parsed): string {
    if (p.mode === 'announce') {
      return p.color === 'primary' ? '/announce' : `/announce (${p.color})`;
    }
    return p.verb ?? '';
  }

  // Substituted segments over the *stripped* body: known tokens become
  // highlighted sample values, unknown {tokens} stay marked so typos are visible.
  type Seg = { text: string; kind: 'plain' | 'sample' | 'unknown' };
  function segmentsOf(body: string, samples: Record<string, string>): Seg[] {
    const out: Seg[] = [];
    const re = /\{([a-z_]+)\}/gi;
    let last = 0;
    for (const m of body.matchAll(re)) {
      if (m.index > last) out.push({ text: body.slice(last, m.index), kind: 'plain' });
      const key = m[1].toLowerCase();
      if (key in samples) out.push({ text: samples[key], kind: 'sample' });
      else out.push({ text: m[0], kind: 'unknown' });
      last = m.index + m[0].length;
    }
    if (last < body.length) out.push({ text: body.slice(last), kind: 'plain' });
    return out;
  }

  // One rehearsed bot message per response line, mirroring the worker's
  // fan-out: every line is parsed for its own slash-verb and substituted
  // independently, capped at the same ceiling the service enforces.
  type LineView = Parsed & { segments: Seg[] };
  const views = $derived.by<LineView[]>(() =>
    responseLines(response)
      .slice(0, RESPONSE_MAX_LINES)
      .map((line) => {
        const p = parseLine(line);
        return { ...p, segments: segmentsOf(p.body, SAMPLES) };
      })
  );

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
    <!-- One bot message per response line, staggered like the worker's send order. -->
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
              <span class="msg reply">
                {#each v.segments as seg, i (i)}
                  {#if seg.kind === 'sample'}<mark>{seg.text}</mark>
                  {:else if seg.kind === 'unknown'}<mark class="unknown" title={t('chatPreview.unknownVar')}>{seg.text}</mark>
                  {:else}{seg.text}{/if}
                {/each}
              </span>
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
            <span class="msg reply action">
              {#each v.segments as seg, i (i)}
                {#if seg.kind === 'sample'}<mark>{seg.text}</mark>
                {:else if seg.kind === 'unknown'}<mark class="unknown" title={t('chatPreview.unknownVar')}>{seg.text}</mark>
                {:else}{seg.text}{/if}
              {/each}
            </span>
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
              <span class="msg reply">
                {#each v.segments as seg, i (i)}
                  {#if seg.kind === 'sample'}<mark>{seg.text}</mark>
                  {:else if seg.kind === 'unknown'}<mark class="unknown" title={t('chatPreview.unknownVar')}>{seg.text}</mark>
                  {:else}{seg.text}{/if}
                {/each}
              </span>
            {:else}
              <span class="msg empty">{t('chatPreview.addMessageAfter', { verb: '/pin' })}</span>
            {/if}
          </div>
        {:else}
          <span class="msg reply">
            {#each v.segments as seg, i (i)}
              {#if seg.kind === 'sample'}<mark>{seg.text}</mark>
              {:else if seg.kind === 'unknown'}<mark class="unknown" title={t('chatPreview.unknownVar')}>{seg.text}</mark>
              {:else}{seg.text}{/if}
            {/each}
          </span>
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
