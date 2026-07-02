<script lang="ts">
  // First-visit onboarding: a short stepper shown once when a new user reaches
  // the dashboard (they're already connected by then, so it starts at the step
  // people actually miss — modding the bot). Dismissal is remembered in
  // localStorage; `?welcome=1` re-opens it for a refresher.
  import { Icon, Modal } from '@bagel/shared';

  let { open = false, onDone }: { open: boolean; onDone: () => void } = $props();

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
      icon: 'moderation' as const,
      title: 'Mod the bot',
      body: 'Type this in your Twitch chat. Without mod status, Twitch silences the bot in follower-only, sub-only, and slow-mode chats — it would look like it just stopped working.',
      mod: true
    },
    {
      icon: 'commands' as const,
      title: 'Create your first command',
      body: 'Head to Commands and build a !command — the live chat rehearsal shows exactly what the bot will say before you save.',
      cta: { href: '/commands', label: 'Open Commands' }
    },
    {
      icon: 'modules' as const,
      title: 'Flip on modules',
      body: 'Optional extras like Auto Shoutout for incoming raids. One toggle each, configured in a minute.',
      cta: { href: '/modules', label: 'Browse modules' }
    }
  ];

  let step = $state(0);
  const last = $derived(step === steps.length - 1);
</script>

<Modal {open} title="Welcome aboard 🥯" closeModal={onDone}>
  <p class="intro">You're connected — the bot can see your chat. Two minutes of setup and it's fully live:</p>

  {#key step}
    {@const cta = steps[step].cta}
    <div class="step">
      <div class="step-head">
        <span class="step-ico"><Icon name={steps[step].icon} size={16} /></span>
        <h4>{steps[step].title}</h4>
      </div>
      <p class="step-body">{steps[step].body}</p>
      {#if steps[step].mod}
        <button type="button" class="mod-cmd" onclick={copyMod} title="Copy to clipboard">
          <code>{MOD_COMMAND}</code>
          <span class="copy-hint">
            <Icon name={copied ? 'check' : 'link'} size={12} />
            {copied ? 'Copied' : 'Copy'}
          </span>
        </button>
      {/if}
      {#if cta}
        <a class="btn ghost step-cta" href={cta.href} onclick={onDone}>
          {cta.label}
        </a>
      {/if}
    </div>
  {/key}

  <div class="foot">
    <div class="dots" aria-label="Step {step + 1} of {steps.length}">
      {#each steps as _, i (i)}
        <button
          type="button"
          class="dot {i === step ? 'on' : ''}"
          aria-label="Go to step {i + 1}"
          onclick={() => (step = i)}
        ></button>
      {/each}
    </div>
    <div class="nav">
      {#if step > 0}
        <button type="button" class="btn ghost" onclick={() => (step -= 1)}>Back</button>
      {:else}
        <button type="button" class="btn ghost" onclick={onDone}>Skip</button>
      {/if}
      {#if last}
        <button type="button" class="btn primary" onclick={onDone}>Done</button>
      {:else}
        <button type="button" class="btn primary" onclick={() => (step += 1)}>Next</button>
      {/if}
    </div>
  </div>
</Modal>

<style>
  .intro {
    font-family: var(--bb-font-body);
    font-size: 13.5px;
    line-height: 1.55;
    color: var(--bb-muted);
    margin: 0 0 16px;
  }

  .step {
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-lg, 16px);
    padding: 18px;
    margin-bottom: 16px;
    animation: step-in 260ms var(--bb-ease-out-expo, ease) both;
  }
  @keyframes step-in {
    from { opacity: 0; transform: translateX(10px); }
    to { opacity: 1; transform: translateX(0); }
  }

  .step-head { display: flex; align-items: center; gap: 10px; margin-bottom: 8px; }
  .step-ico {
    display: inline-flex; align-items: center; justify-content: center;
    width: 32px; height: 32px; border-radius: var(--bb-radius-md, 10px); flex: none;
    background: rgba(82, 183, 136, 0.12); border: 1px solid rgba(82, 183, 136, 0.3);
    color: var(--bb-green-glow);
  }
  .step-head h4 {
    font-family: var(--bb-font-display); font-weight: 700; font-size: 16px;
    letter-spacing: -0.01em; color: var(--bb-white); margin: 0;
  }
  .step-body {
    font-family: var(--bb-font-body); font-size: 13px; line-height: 1.55;
    color: var(--bb-muted); margin: 0;
  }

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

  .step-cta { display: inline-flex; margin-top: 12px; text-decoration: none; }

  .foot { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
  .dots { display: flex; gap: 7px; }
  .dot {
    width: 8px; height: 8px; border-radius: 50%; padding: 0;
    background: rgba(240, 236, 228, 0.18); border: none; cursor: pointer;
    transition: background var(--bb-dur-fast, 180ms) ease, transform var(--bb-dur-fast, 180ms) var(--bb-ease-out-back, ease);
  }
  .dot.on { background: var(--bb-tan); transform: scale(1.25); }
  .nav { display: flex; gap: 8px; }

  @media (prefers-reduced-motion: reduce) {
    .step { animation: none; }
  }
</style>
