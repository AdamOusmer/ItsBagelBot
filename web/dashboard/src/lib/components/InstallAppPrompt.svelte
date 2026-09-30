<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  import { browser } from '$app/environment';
  import { page } from '$app/state';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Popover from '@bagel/ui/svelte/Popover.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';

  const { t } = getI18n();

  interface BeforeInstallPromptEvent extends Event {
    readonly platforms: string[];
    readonly userChoice: Promise<{ outcome: 'accepted' | 'dismissed'; platform: string }>;
    prompt(): Promise<void>;
  }

  const DISMISS_KEY = 'bagel:install-dismissed';
  const REOFFER_MS = 30 * 24 * 60 * 60 * 1000;
  const SHOW_DELAY_MS = 4000;

  let promptEvent: BeforeInstallPromptEvent | null = null;
  let mode = $state<'chromium' | 'ios' | null>(null);
  let visible = $state(false);
  let ready = $state(false);
  let iosOpen = $state(false);

  const inApp = $derived(page.route.id?.startsWith('/(app)') ?? false);

  function isStandalone(): boolean {
    const nav = window.navigator as Navigator & { standalone?: boolean };
    return window.matchMedia('(display-mode: standalone)').matches || nav.standalone === true;
  }

  function isIos(): boolean {
    const ua = window.navigator.userAgent;
    const iPadOS = /Macintosh/.test(ua) && window.navigator.maxTouchPoints > 1;
    return /iPhone|iPad|iPod/.test(ua) || iPadOS;
  }

  function isDismissed(): boolean {
    try {
      const dismissedAt = Number(localStorage.getItem(DISMISS_KEY));
      return Date.now() - dismissedAt < REOFFER_MS;
    } catch {
      return false;
    }
  }

  function forceShow(): boolean {
    return page.url?.searchParams.get('install') === '1';
  }

  function persistDismissed(): void {
    try {
      localStorage.setItem(DISMISS_KEY, String(Date.now()));
    } catch {
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
    e.preventDefault();
    promptEvent = e as BeforeInstallPromptEvent;
    show('chromium');
  }

  function onInstalled(): void {
    hide();
  }

  async function runInstall(): Promise<void> {
    const evt = promptEvent;
    if (!evt) return;
    await evt.prompt();
    const choice = await evt.userChoice;
    promptEvent = null;
    if (choice.outcome === 'dismissed') dismiss();
    else hide();
  }

  onMount(() => {
    if (isStandalone()) return;
    if (isDismissed() && !forceShow()) return;

    window.addEventListener('beforeinstallprompt', onBeforeInstall);
    window.addEventListener('appinstalled', onInstalled);

    if (isIos()) show('ios');

    const arm = () => (ready = true);
    const timer = setTimeout(arm, forceShow() ? 0 : SHOW_DELAY_MS);
    window.addEventListener('pointerdown', arm, { once: true });
    window.addEventListener('keydown', arm, { once: true });

    return () => {
      clearTimeout(timer);
      window.removeEventListener('pointerdown', arm);
      window.removeEventListener('keydown', arm);
      window.removeEventListener('beforeinstallprompt', onBeforeInstall);
      window.removeEventListener('appinstalled', onInstalled);
    };
  });
</script>

{#if browser && visible && ready && inApp}
  <Popover
    bind:open={iosOpen}
    placement="bottom"
    expands={mode === 'ios'}
    onActivate={runInstall}
    label={t('install.ariaLabel')}
    title={t('install.ios.title')}
    closeLabel={t('install.ios.close')}
    dismissLabel={t('install.dismiss')}
    onDismiss={dismiss}
  >
    {#snippet pill()}
      <Icon name="home" size={15} />
      <span>{t('install.cta')}</span>
    {/snippet}

    <ol class="steps">
      <li>
        <span class="glyph" aria-hidden="true"><Icon name="share" size={17} strokeWidth={1.7} /></span>
        <Text as="span" size="sm">{t('install.ios.step1')}</Text>
      </li>
      <li>
        <span class="glyph" aria-hidden="true"><Icon name="addSquare" size={17} strokeWidth={1.7} /></span>
        <Text as="span" size="sm">{t('install.ios.step2')}</Text>
      </li>
    </ol>

    <Button variant="secondary" block onclick={() => (iosOpen = false)} tone="success">{t('common.gotIt')}</Button>
  </Popover>
{/if}

<style>
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
  }
  .glyph {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 30px;
    height: 30px;
    flex: none;
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-sm);
    color: var(--bb-tan-light);
  }
</style>
