<script lang="ts">
  // A floating "Install app" pill that lets phone users add the dashboard to
  // their home screen as a PWA. Two install paths are supported:
  //   - Chromium (Android/desktop): captures the `beforeinstallprompt` event and
  //     drives the native install flow on click.
  //   - iOS Safari: no such event exists, so the pill opens a small popover with
  //     the manual "Share → Add to Home Screen" steps.
  // Everything is progressive enhancement: the component renders nothing on the
  // server and only appears once a real install path is detected in the browser.
  import { onMount } from 'svelte';
  import { browser } from '$app/environment';
  import { page } from '$app/state';
  import { Icon, getI18n } from '@bagel/shared';

  const { t } = getI18n();

  // Minimal shape of the non-standard Chromium event (not in lib.dom yet).
  interface BeforeInstallPromptEvent extends Event {
    readonly platforms: string[];
    readonly userChoice: Promise<{ outcome: 'accepted' | 'dismissed'; platform: string }>;
    prompt(): Promise<void>;
  }

  // Persisted "don't ask again" flag. The marketing site can override it by
  // deep-linking with ?install=1 (see forceShow).
  const DISMISS_KEY = 'bagel:install-dismissed';

  const uid = $props.id();
  const titleId = `install-ios-${uid}`;

  // The deferred Chromium prompt, stashed until the user opts in.
  let promptEvent: BeforeInstallPromptEvent | null = null;
  // Which install path is active; drives what the pill click does.
  let mode = $state<'chromium' | 'ios' | null>(null);
  let visible = $state(false);
  let iosOpen = $state(false);
  let closeBtn = $state<HTMLButtonElement | null>(null);

  // Already running as an installed PWA — nothing to offer.
  function isStandalone(): boolean {
    const nav = window.navigator as Navigator & { standalone?: boolean };
    return window.matchMedia('(display-mode: standalone)').matches || nav.standalone === true;
  }

  // iPhone/iPad, including iPadOS 13+ which masquerades as "Macintosh" but
  // reports touch points.
  function isIos(): boolean {
    const ua = window.navigator.userAgent;
    const iPadOS = /Macintosh/.test(ua) && window.navigator.maxTouchPoints > 1;
    return /iPhone|iPad|iPod/.test(ua) || iPadOS;
  }

  function isDismissed(): boolean {
    try {
      return localStorage.getItem(DISMISS_KEY) === '1';
    } catch {
      return false;
    }
  }

  // Marketing deep-link that re-surfaces the pill even after a dismissal.
  function forceShow(): boolean {
    return page.url?.searchParams.get('install') === '1';
  }

  function persistDismissed(): void {
    try {
      localStorage.setItem(DISMISS_KEY, '1');
    } catch {
      // Storage unavailable (private mode): the dismissal simply won't stick.
    }
  }

  function show(next: 'chromium' | 'ios'): void {
    mode = next;
    visible = true;
  }

  function hide(): void {
    visible = false;
    iosOpen = false;
  }

  function dismiss(): void {
    persistDismissed();
    hide();
  }

  function onBeforeInstall(e: Event): void {
    // Suppress Chrome's default mini-infobar; we present our own pill instead.
    e.preventDefault();
    promptEvent = e as BeforeInstallPromptEvent;
    show('chromium');
  }

  function onInstalled(): void {
    hide();
  }

  // Chromium: replay the stored prompt and honour the user's choice.
  async function runInstall(): Promise<void> {
    const evt = promptEvent;
    if (!evt) return;
    await evt.prompt();
    const choice = await evt.userChoice;
    promptEvent = null;
    if (choice.outcome === 'dismissed') dismiss();
    else hide();
  }

  async function onPillClick(): Promise<void> {
    if (mode === 'ios') {
      iosOpen = true;
      return;
    }
    await runInstall();
  }

  function onKeydown(e: KeyboardEvent): void {
    if (e.key === 'Escape' && iosOpen) iosOpen = false;
  }

  onMount(() => {
    if (isStandalone()) return;
    if (isDismissed() && !forceShow()) return;

    window.addEventListener('beforeinstallprompt', onBeforeInstall);
    window.addEventListener('appinstalled', onInstalled);

    // iOS never fires beforeinstallprompt, so offer the manual path right away.
    if (isIos()) show('ios');

    return () => {
      window.removeEventListener('beforeinstallprompt', onBeforeInstall);
      window.removeEventListener('appinstalled', onInstalled);
    };
  });

  // Move focus into the popover when it opens so keyboard users land on a control.
  $effect(() => {
    if (iosOpen) closeBtn?.focus();
  });
</script>

<svelte:window onkeydown={onKeydown} />

{#if browser && visible}
  <div class="install-root">
    <div class="pill">
      <button
        class="pill-cta"
        type="button"
        aria-label={t('install.ariaLabel')}
        aria-haspopup={mode === 'ios' ? 'dialog' : undefined}
        aria-expanded={mode === 'ios' ? iosOpen : undefined}
        onclick={onPillClick}
      >
        <Icon name="home" size={15} />
        <span>{t('install.cta')}</span>
      </button>
      <button class="pill-x" type="button" aria-label={t('install.dismiss')} onclick={dismiss}>
        <Icon name="x" size={13} />
      </button>
    </div>

    {#if iosOpen}
      <div class="sheet" role="dialog" aria-modal="false" aria-labelledby={titleId}>
        <div class="sheet-head">
          <h2 id={titleId}>{t('install.ios.title')}</h2>
          <button
            class="sheet-x"
            type="button"
            aria-label={t('install.ios.close')}
            onclick={() => (iosOpen = false)}
            bind:this={closeBtn}
          >
            <Icon name="x" size={14} />
          </button>
        </div>

        <ol class="steps">
          <li>
            <span class="glyph" aria-hidden="true">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
                <path d="M12 15V4" />
                <path d="M8 8l4-4 4 4" />
                <path d="M6 12v6a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2v-6" />
              </svg>
            </span>
            <span>{t('install.ios.step1')}</span>
          </li>
          <li>
            <span class="glyph" aria-hidden="true">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
                <rect x="4" y="4" width="16" height="16" rx="4" />
                <path d="M12 9v6M9 12h6" />
              </svg>
            </span>
            <span>{t('install.ios.step2')}</span>
          </li>
        </ol>

        <button class="sheet-done" type="button" onclick={() => (iosOpen = false)}>
          {t('common.gotIt')}
        </button>
      </div>
    {/if}
  </div>
{/if}

<style>
  /* Anchored top-right, clear of the sticky topbar and notch. Sits above the
     dock (z 60/61) but below topbar menus (89/90), modals (200) and toasts. */
  .install-root {
    position: fixed;
    top: calc(env(safe-area-inset-top, 0px) + 64px);
    right: max(14px, env(safe-area-inset-right, 0px));
    z-index: 70;
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    font-family: var(--bb-font-body);
  }

  .pill {
    display: inline-flex;
    align-items: center;
    gap: 2px;
    padding: 3px 3px 3px 4px;
    background: var(--bb-card-bg, #111110);
    border: 1px solid var(--bb-border-strong, rgba(201, 168, 124, 0.35));
    border-radius: var(--bb-radius-pill, 999px);
    box-shadow: 0 14px 40px rgba(0, 0, 0, 0.5);
    animation: pill-in 260ms var(--bb-ease-out-back, ease-out) both;
  }

  .pill-cta {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 7px 12px;
    border: none;
    background: none;
    border-radius: var(--bb-radius-pill, 999px);
    cursor: pointer;
    font: inherit;
    font-size: 13px;
    font-weight: 600;
    color: var(--bb-white);
    transition: background var(--bb-dur-fast, 180ms) var(--bb-ease-out-expo, ease);
  }
  .pill-cta :global(svg) {
    stroke: var(--bb-green-glow, #52b788);
    fill: none;
    stroke-width: 1.8;
  }
  .pill-cta:hover {
    background: rgba(82, 183, 136, 0.12);
  }

  .pill-x {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    flex: none;
    border: none;
    background: none;
    border-radius: 50%;
    cursor: pointer;
    color: var(--bb-muted);
    transition:
      color var(--bb-dur-fast, 180ms) ease,
      background var(--bb-dur-fast, 180ms) ease;
  }
  .pill-x :global(svg) {
    stroke: currentColor;
    fill: none;
    stroke-width: 1.8;
  }
  .pill-x:hover {
    color: var(--bb-white);
    background: rgba(255, 255, 255, 0.06);
  }

  /* Non-modal instructions popover, anchored under the pill. */
  .sheet {
    margin-top: 8px;
    width: min(300px, calc(100vw - 28px));
    padding: 16px;
    background: var(--bb-card-bg, #111110);
    border: 1px solid var(--bb-border-strong, rgba(201, 168, 124, 0.35));
    border-radius: var(--bb-radius-md, 12px);
    box-shadow: 0 18px 50px rgba(0, 0, 0, 0.55);
    animation: sheet-in 240ms var(--bb-ease-out-back, ease-out) both;
  }

  .sheet-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    margin-bottom: 12px;
  }
  .sheet-head h2 {
    margin: 0;
    font-family: var(--bb-font-display, sans-serif);
    font-weight: 700;
    font-size: 15px;
    color: var(--bb-white);
  }
  .sheet-x {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    flex: none;
    border: none;
    background: none;
    border-radius: 8px;
    cursor: pointer;
    color: var(--bb-muted);
  }
  .sheet-x :global(svg) {
    stroke: currentColor;
    fill: none;
    stroke-width: 1.8;
  }
  .sheet-x:hover {
    color: var(--bb-white);
    background: rgba(255, 255, 255, 0.06);
  }

  .steps {
    list-style: none;
    margin: 0 0 14px;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .steps li {
    display: flex;
    align-items: center;
    gap: 11px;
    font-size: 13px;
    line-height: 1.4;
    color: var(--bb-white);
  }
  .glyph {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 30px;
    height: 30px;
    flex: none;
    border: 1px solid var(--bb-border, rgba(201, 168, 124, 0.15));
    border-radius: 8px;
    color: var(--bb-tan-light, #e0c49a);
  }
  .glyph svg {
    width: 17px;
    height: 17px;
  }

  .sheet-done {
    width: 100%;
    min-height: 40px;
    border: 1px solid rgba(82, 183, 136, 0.4);
    background: rgba(82, 183, 136, 0.12);
    border-radius: var(--bb-radius-sm, 6px);
    cursor: pointer;
    font: inherit;
    font-size: 13px;
    font-weight: 600;
    color: var(--bb-green-glow, #52b788);
    transition: background var(--bb-dur-fast, 180ms) ease;
  }
  .sheet-done:hover {
    background: rgba(82, 183, 136, 0.2);
  }

  @keyframes pill-in {
    from {
      opacity: 0;
      transform: translateY(-8px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
  @keyframes sheet-in {
    from {
      opacity: 0;
      transform: translateY(-6px) scale(0.98);
    }
    to {
      opacity: 1;
      transform: translateY(0) scale(1);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .pill,
    .sheet {
      animation: none;
    }
  }
</style>
