<script lang="ts">
  import '../app.css';
  import { RootShell } from '@bagel/shared';
  let { data, children } = $props();

  const DEFAULT_TITLE = 'ItsBagelBot Dashboard';
  const DEFAULT_DESC =
    'Sign in to configure moderation, custom commands, timers, and channel points for your Twitch channel.';
</script>

<svelte:head>
  <title>{DEFAULT_TITLE}</title>
  <meta name="description" content={DEFAULT_DESC} />
  <meta property="og:title" content={DEFAULT_TITLE} />
  <meta property="og:description" content={DEFAULT_DESC} />
  <meta name="twitter:title" content={DEFAULT_TITLE} />
  <meta name="twitter:description" content={DEFAULT_DESC} />
</svelte:head>

<RootShell locale={data.locale}>{@render children()}</RootShell>
