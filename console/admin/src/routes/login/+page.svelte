<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { page } from '$app/state';
  import AuroraBg from '@bagel/shared/components/AuroraBg.svelte';
  import LightField from '@bagel/shared/components/LightField.svelte';
  import { getI18n } from '@bagel/shared/i18n/context';

  const { t } = getI18n();

  // The `e` query parameter is written by the OAuth callback, which is where
  // these four cases are decided. A table rather than a chain: the callback can
  // grow a fifth reason, and an unrecognised one must fall through to no notice
  // rather than to the wrong one.
  const MESSAGES = {
    denied: 'admin.login.errDenied',
    state: 'admin.login.errState',
    oauth: 'admin.login.errOauth',
    scope: 'admin.login.errScope'
  } as const;

  const err = $derived(page.url.searchParams.get('e') ?? '');
  const noticeKey = $derived(MESSAGES[err as keyof typeof MESSAGES]);
  const notice = $derived(noticeKey ? t(noticeKey) : undefined);
</script>

<AuroraBg />
<div class="starfield" aria-hidden="true"><LightField /></div>

<main class="login">
  <div class="panel">
    <img src="/logo.png" alt={t('common.appName')} />
    <div class="name">{t('common.appName')}</div>
    <div class="sub">{t('admin.login.sub')}</div>
    {#if notice}<p class="notice">{notice}</p>{/if}
    <p class="lede">{t('admin.login.lede')}</p>
    <a href="/auth/login" class="btn primary twitch">
      <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
        <path
          fill="currentColor"
          d="M4 3h17v11l-5 5h-4l-3 3H6v-3H2V7l2-4Zm15 10V5H6v11h4v3l3-3h6l0-3Zm-4-5h2v5h-2V8Zm-5 0h2v5h-2V8Z"
        />
      </svg>
      {t('admin.login.cta')}
    </a>
  </div>
</main>

<style>
  /* Mote field sits above the aurora (z-index 0) but below the panel (z-index 1).
     Own stacking context so LightField's z-index:-1 canvas stays contained. */
  .starfield {
    position: fixed;
    inset: 0;
    z-index: 0;
    pointer-events: none;
  }

  .login {
    position: relative;
    z-index: 1;
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px;
  }
  .panel {
    width: 100%;
    max-width: 420px;
    padding: 44px 40px 40px;
    text-align: center;
    background: var(--glass-fill);
    border: 1px solid var(--glass-border);
    border-radius: var(--bb-radius-md);
    backdrop-filter: blur(var(--glass-blur)) saturate(var(--glass-sat, 180%));
    box-shadow: var(--glass-rim), var(--glass-shadow);
  }
  .panel img {
    width: 44px;
    height: 44px;
    border-radius: var(--bb-radius-sm);
    margin-bottom: 16px;
  }
  .name {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 22px;
    letter-spacing: -0.01em;
    color: var(--bb-white);
  }
  .sub {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: 0.18em;
    text-transform: uppercase;
    color: var(--bb-muted);
    margin-top: 6px;
  }
  .notice {
    font-family: var(--bb-font-mono);
    font-size: 12px;
    color: #cf8a78;
    margin: 16px 0 0;
  }
  .lede {
    font-family: var(--bb-font-body);
    font-size: 14px;
    line-height: 1.55;
    color: var(--bb-muted);
    margin: 18px 0 24px;
  }
  .twitch {
    width: 100%;
    justify-content: center;
    background: #9146ff;
    color: #fff;
    border-color: #9146ff;
  }
  .twitch:hover {
    background: #7d2ff5;
    border-color: #7d2ff5;
    box-shadow: 0 0 24px rgba(145, 70, 255, 0.35);
  }
</style>
