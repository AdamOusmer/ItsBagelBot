<script lang="ts">
  import { page } from '$app/state';

  const err = $derived(page.url.searchParams.get('e'));
  const messages: Record<string, string> = {
    denied: 'That Twitch account is not on the staff allowlist.',
    state: 'Sign-in expired or was tampered with. Try again.',
    oauth: 'Twitch rejected the sign-in. Try again.',
    scope: 'Missing required permission.'
  };
  const notice = $derived(err ? messages[err] : undefined);
</script>

<main class="login">
  <div class="panel">
    <img src="/logo.png" alt="ItsBagelBot" />
    <div class="name">ItsBagelBot</div>
    <div class="sub">Admin Console</div>
    {#if notice}<p class="notice">{notice}</p>{/if}
    <p class="lede">Operator access. Sign in with the Twitch account on the staff allowlist.</p>
    <a href="/auth/login" class="btn primary twitch">
      <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
        <path
          fill="currentColor"
          d="M4 3h17v11l-5 5h-4l-3 3H6v-3H2V7l2-4Zm15 10V5H6v11h4v3l3-3h6l0-3Zm-4-5h2v5h-2V8Zm-5 0h2v5h-2V8Z"
        />
      </svg>
      Sign in with Twitch
    </a>
  </div>
</main>

<style>
  .login {
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
    border-radius: 8px;
    backdrop-filter: blur(var(--glass-blur)) saturate(var(--glass-sat, 180%));
    -webkit-backdrop-filter: blur(var(--glass-blur)) saturate(var(--glass-sat, 180%));
    box-shadow: var(--glass-rim), var(--glass-shadow);
  }
  .panel img {
    width: 44px;
    height: 44px;
    border-radius: 8px;
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
