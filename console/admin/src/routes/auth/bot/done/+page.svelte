<script lang="ts">
  import { page } from '$app/stores';

  const ok = $derived($page.url.searchParams.get('ok') === '1');
  const err = $derived($page.url.searchParams.get('e'));

  const MSG: Record<string, string> = {
    state: 'The authorization link expired or was invalid. Generate a new one from the admin console.',
    oauth: 'Twitch rejected the authorization. Please try again.',
    account: 'That Twitch account is not the configured bot account.',
    config: 'Bot authorization is disabled until ADMIN_BOT_USER_ID is configured.'
  };
</script>

<main>
  {#if ok}
    <h1>Bot authorized</h1>
    <p>The bot account token was stored. You can close this tab.</p>
  {:else}
    <h1>Authorization failed</h1>
    <p>{(err && MSG[err]) ?? 'Something went wrong. Generate a new link and try again.'}</p>
  {/if}
</main>

<style>
  main {
    max-width: 460px;
    margin: 18vh auto;
    padding: 0 24px;
    font-family: var(--bb-font-body, system-ui, sans-serif);
    text-align: center;
  }
  h1 { font-size: 1.4rem; margin: 0 0 8px; }
  p { color: var(--bb-muted, #888); margin: 0; }
</style>
