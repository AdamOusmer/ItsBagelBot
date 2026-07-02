<script lang="ts">
  // Live chat rehearsal: acts out the command as it will look in Twitch chat —
  // a viewer types the trigger, the bot "types" for a beat, then replies with
  // sample values substituted into the tokens. Re-runs (debounced) as the
  // response is edited, so authors see the real thing, not a template string.
  import { normName } from '@bagel/shared';

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

  // Substituted segments: known tokens become highlighted sample values,
  // unknown {tokens} stay marked so typos are visible.
  type Seg = { text: string; kind: 'plain' | 'sample' | 'unknown' };
  const segments = $derived.by<Seg[]>(() => {
    const out: Seg[] = [];
    const re = /\{([a-z_]+)\}/gi;
    let last = 0;
    for (const m of response.matchAll(re)) {
      if (m.index > last) out.push({ text: response.slice(last, m.index), kind: 'plain' });
      const key = m[1].toLowerCase();
      if (key in SAMPLES) out.push({ text: SAMPLES[key], kind: 'sample' });
      else out.push({ text: m[0], kind: 'unknown' });
      last = m.index + m[0].length;
    }
    if (last < response.length) out.push({ text: response.slice(last), kind: 'plain' });
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

<div class="chat" aria-label="Chat preview">
  <span class="chat-tag">Chat rehearsal</span>
  <div class="line viewer">
    <span class="who viewer-name">sesame_sam</span>
    <span class="msg">{trigger}</span>
  </div>
  <div class="line bot">
    <span class="who bot-name">
      <img src="/logo.png" alt="" class="bot-avatar" />
      ItsBagelBot
    </span>
    {#if typing}
      <span class="msg typing" aria-label="Bot is typing">
        <span class="tdot"></span><span class="tdot"></span><span class="tdot"></span>
      </span>
    {:else if response.trim()}
      <span class="msg reply">
        {#each segments as seg, i (i)}
          {#if seg.kind === 'sample'}<mark>{seg.text}</mark>
          {:else if seg.kind === 'unknown'}<mark class="unknown" title="Unknown variable">{seg.text}</mark>
          {:else}{seg.text}{/if}
        {/each}
      </span>
    {:else}
      <span class="msg empty">…the bot has nothing to say yet</span>
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
    .reply { animation: none; }
    .tdot { animation: none; }
  }
</style>
