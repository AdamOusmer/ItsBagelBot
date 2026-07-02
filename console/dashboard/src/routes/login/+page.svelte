<script lang="ts">
  import { page } from '$app/state';
  import { AuroraBg, Icon } from '@bagel/shared';

  const headline = ['Your', 'stream.', 'Your', 'tools.', 'Your', 'rules.'];

  // Why the visitor bounced back here, when the app sent them with a reason.
  const NOTICES: Record<string, string> = {
    signedout: 'You were signed out — that account no longer exists on ItsBagelBot.',
    banned: 'This account can no longer use the console.',
    link: 'That share link is no longer valid. Ask the broadcaster for a new one.'
  };
  const notice = $derived(NOTICES[page.url.searchParams.get('e') ?? ''] ?? null);

  // The one setup step people miss: without mod status Twitch silences the bot
  // in follower-only / sub-only / slow chats. Make the command one tap to copy.
  const MOD_COMMAND = '/mod ItsBagelBot';
  let copied = $state(false);
  async function copyMod() {
    let ok = false;
    try {
      await navigator.clipboard.writeText(MOD_COMMAND);
      ok = true;
    } catch {
      // Clipboard API blocked (permissions/insecure context): legacy fallback.
      const ta = document.createElement('textarea');
      ta.value = MOD_COMMAND;
      ta.style.position = 'fixed';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.select();
      try {
        ok = document.execCommand('copy');
      } catch {
        ok = false;
      }
      ta.remove();
    }
    if (ok) {
      copied = true;
      setTimeout(() => (copied = false), 2000);
    }
  }

  const steps = [
    {
      icon: 'power' as const,
      title: 'Connect with Twitch',
      body: 'One click. You grant the bot permission to read and reply in your channel — nothing else.'
    },
    {
      icon: 'moderation' as const,
      title: 'Mod the bot',
      body: 'Type the command below in your chat. Without mod status, Twitch silences the bot in follower-only, sub-only, and slow-mode chats.',
      mod: true
    },
    {
      icon: 'commands' as const,
      title: 'Build your commands',
      body: 'Create !commands with live chat previews, flip modules on, and the bot starts replying right away.'
    }
  ];
</script>

<AuroraBg />

<main class="onboard">
  {#if notice}
    <div class="notice reveal" style="--d:0s" role="alert">
      <Icon name="ban" size={14} />
      {notice}
    </div>
  {/if}

  <div class="logo pop" style="--d:0s">
    <img src="/logo.png" alt="ItsBagelBot" />
    <span class="halo"></span>
  </div>

  <div class="eyebrow reveal" style="--d:.18s">ItsBagelBot Console</div>

  <h1 class="hero">
    {#each headline as w, i}
      <span class="word reveal" style="--d:{0.28 + i * 0.07}s" class:accent={i >= 4}>{w}&nbsp;</span>
    {/each}
  </h1>

  <p class="lede reveal" style="--d:.85s">
    Private, distributed, encrypted. Manage commands and your bot from one warm console.
  </p>

  <a class="cta reveal" style="--d:1s" href="/auth/login">
    <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 3h16v12l-4 4h-4l-3 3v-3H4z" /><line x1="9" y1="8" x2="9" y2="12" /><line x1="14" y1="8" x2="14" y2="12" /></svg>
    Continue with Twitch
  </a>

  <!-- Getting set up: the three steps, with the mod command front and center. -->
  <section class="steps" aria-label="How it works">
    <span class="steps-eyebrow reveal" style="--d:1.15s">Set up in two minutes</span>
    <div class="step-grid">
      {#each steps as step, i (step.title)}
        <div class="step reveal" style="--d:{1.25 + i * 0.12}s">
          <div class="step-top">
            <span class="step-ico"><Icon name={step.icon} size={16} /></span>
            <span class="step-n">{i + 1}</span>
          </div>
          <h3>{step.title}</h3>
          <p>{step.body}</p>
          {#if step.mod}
            <button type="button" class="mod-cmd" onclick={copyMod} title="Copy to clipboard">
              <code>{MOD_COMMAND}</code>
              <span class="copy-hint">
                <Icon name={copied ? 'check' : 'link'} size={12} />
                {copied ? 'Copied' : 'Copy'}
              </span>
            </button>
          {/if}
        </div>
      {/each}
    </div>
  </section>

  <div class="features reveal" style="--d:1.7s">
    <span class="feat">Zero downtime</span>
    <span class="feat">End-to-end encrypted</span>
    <span class="feat">Edge-routed, 3 regions</span>
  </div>
</main>

<style>
  .onboard {
    position: relative;
    z-index: 1;
    min-height: 100vh;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    text-align: center;
    gap: 18px;
    padding: 48px 24px;
  }

  /* Entrance is CSS-only: content animates in on load with no JS dependency, so
     a reload never leaves the page invisible. `both` holds the from-state during
     the stagger delay, then settles visible. */
  .reveal {
    opacity: 0;
    animation: reveal 0.8s cubic-bezier(0.16, 1, 0.3, 1) both;
    animation-delay: var(--d, 0s);
  }
  .pop {
    opacity: 0;
    animation: pop 0.9s cubic-bezier(0.16, 1, 0.3, 1) both;
    animation-delay: var(--d, 0s);
  }
  @keyframes reveal { from { opacity: 0; transform: translateY(24px); } to { opacity: 1; transform: none; } }
  @keyframes pop { from { opacity: 0; transform: scale(0.6); filter: blur(12px); } to { opacity: 1; transform: none; filter: none; } }

  .notice {
    display: inline-flex;
    align-items: center;
    gap: 9px;
    padding: 11px 18px;
    border-radius: var(--bb-radius-md, 10px);
    background: rgba(176, 90, 70, 0.1);
    border: 1px solid rgba(176, 90, 70, 0.4);
    color: #cf8a78;
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    max-width: 60ch;
  }
  .notice :global(svg) { stroke: currentColor; fill: none; stroke-width: 1.8; flex: none; }

  .logo { position: relative; width: 84px; height: 84px; display: grid; place-items: center; margin-bottom: 6px; }
  .logo img { width: 72px; height: 72px; border-radius: 18px; animation: float 6s ease-in-out infinite; }
  .logo .halo { position: absolute; inset: -22px; border-radius: 50%; background: radial-gradient(circle, rgba(82, 183, 136, 0.35), transparent 68%); filter: blur(8px); animation: pulse 3.2s ease-in-out infinite; }

  .eyebrow { font-family: var(--bb-font-mono); font-size: 0.72rem; letter-spacing: 0.14em; text-transform: uppercase; color: var(--bb-green-glow); }
  .hero { font-family: var(--bb-font-display); font-weight: 800; font-size: clamp(40px, 8vw, 92px); line-height: 0.98; letter-spacing: -0.02em; color: var(--bb-white); margin: 4px 0; max-width: 12ch; }
  .hero .word { display: inline-block; }
  .hero .accent { color: var(--bb-tan-light); font-style: normal; }

  .lede { font-family: var(--bb-font-body); font-size: clamp(15px, 1.6vw, 18px); color: var(--bb-muted); max-width: 52ch; line-height: 1.6; margin: 4px 0 6px; }

  /* Primary CTA: the marketing site's tan MainButton language. */
  .cta {
    display: inline-flex; align-items: center; gap: 10px;
    font-family: var(--bb-font-display); font-weight: 700; font-size: 15px; letter-spacing: -0.01em;
    color: #0a0a0a; background: var(--bb-tan);
    padding: 16px 32px; border-radius: var(--bb-radius-md, 10px); text-decoration: none;
    transition: background 0.25s, transform 0.25s, box-shadow 0.25s;
  }
  .cta svg { width: 17px; height: 17px; stroke: currentColor; fill: none; stroke-width: 1.8; stroke-linejoin: round; }
  .cta:hover { background: var(--bb-tan-light); transform: translateY(-2px); box-shadow: 0 8px 32px rgba(201, 168, 124, 0.3); }

  /* onboarding steps */
  .steps { margin-top: 34px; width: 100%; max-width: 860px; }
  .steps-eyebrow {
    display: block;
    font-family: var(--bb-font-mono); font-size: 0.72rem; letter-spacing: 0.14em;
    text-transform: uppercase; color: var(--bb-green-glow); margin-bottom: 18px;
  }
  .step-grid {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 14px;
    text-align: left;
  }
  .step {
    background: var(--bb-card-bg, #111110);
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-lg, 16px);
    padding: 22px;
    transition: border-color 360ms var(--bb-ease-out-expo), transform 360ms var(--bb-ease-out-expo);
  }
  @media (hover: hover) and (pointer: fine) {
    .step:hover { border-color: rgba(201, 168, 124, 0.38); transform: translateY(-3px); }
  }
  .step-top { display: flex; align-items: center; justify-content: space-between; margin-bottom: 14px; }
  .step-ico {
    display: inline-flex; align-items: center; justify-content: center;
    width: 32px; height: 32px; border-radius: var(--bb-radius-md, 10px);
    background: rgba(82, 183, 136, 0.12); border: 1px solid rgba(82, 183, 136, 0.3);
    color: var(--bb-green-glow);
  }
  .step-ico :global(svg) { stroke: currentColor; fill: none; stroke-width: 1.6; }
  .step-n { font-family: var(--bb-font-display); font-weight: 800; font-size: 22px; color: var(--bb-border-strong); }
  .step h3 { font-family: var(--bb-font-display); font-weight: 700; font-size: 16px; letter-spacing: -0.01em; color: var(--bb-white); margin: 0 0 8px; }
  .step p { font-family: var(--bb-font-body); font-size: 13px; line-height: 1.55; color: var(--bb-muted); margin: 0; }

  .mod-cmd {
    display: flex; align-items: center; justify-content: space-between; gap: 10px;
    width: 100%; margin-top: 12px; padding: 10px 12px;
    background: rgba(0, 0, 0, 0.35);
    border: 1px dashed var(--bb-border-strong);
    border-radius: var(--bb-radius-md, 10px);
    cursor: pointer;
    transition: border-color 0.2s, background 0.2s;
  }
  .mod-cmd:hover { border-color: var(--bb-tan); background: rgba(201, 168, 124, 0.06); }
  .mod-cmd code { font-family: var(--bb-font-mono); font-size: 13px; color: var(--bb-tan-light); }
  .copy-hint {
    display: inline-flex; align-items: center; gap: 5px;
    font-family: var(--bb-font-body); font-weight: 600; font-size: 11.5px;
    color: var(--bb-muted);
  }
  .mod-cmd:hover .copy-hint { color: var(--bb-tan-pale); }
  .copy-hint :global(svg) { stroke: currentColor; fill: none; stroke-width: 1.8; }

  .features { display: flex; gap: 10px; flex-wrap: wrap; justify-content: center; margin-top: 22px; }
  .feat { font-family: var(--bb-font-body); font-weight: 600; font-size: 12px; color: var(--bb-tan); padding: 8px 16px; border-radius: var(--bb-radius-pill, 100px); background: rgba(255, 255, 255, 0.03); border: 1px solid var(--bb-border); }

  @keyframes float { 0%, 100% { transform: translateY(0) rotate(-1deg); } 50% { transform: translateY(-10px) rotate(1deg); } }
  @keyframes pulse { 0%, 100% { opacity: 0.55; transform: scale(1); } 50% { opacity: 0.9; transform: scale(1.12); } }

  @media (max-width: 760px) {
    .step-grid { grid-template-columns: 1fr; }
    .steps { margin-top: 22px; }
  }

  @media (prefers-reduced-motion: reduce) {
    .reveal, .pop { animation: none; opacity: 1; }
    .logo img, .logo .halo { animation: none; }
  }
</style>
