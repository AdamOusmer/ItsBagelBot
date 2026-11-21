<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { page } from '$app/state';
  import { getI18n } from '@bagel/kit/i18n/context';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Container from '@bagel/ui/svelte/Container.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';

  const { locale, t } = getI18n();

  const reasons = new Map([
    ['state', 'admin.botAuth.errState'],
    ['oauth', 'admin.botAuth.errOauth'],
    ['scope', 'admin.botAuth.errScope'],
    ['account', 'admin.botAuth.errAccount'],
    ['config', 'admin.botAuth.errConfig'],
    ['save', 'admin.botAuth.errSave']
  ]);

  const ok = $derived(page.url.searchParams.get('ok') === '1');
  const reason = $derived(reasons.get(page.url.searchParams.get('e') ?? ''));
  const title = $derived(t(ok ? 'admin.botAuth.okTitle' : 'admin.botAuth.failTitle'));
  const body = $derived(t(ok ? 'admin.botAuth.okBody' : reason ?? 'admin.botAuth.errGeneric'));
</script>

<svelte:head>
  <title>{title} · ItsBagelBot</title>
</svelte:head>

<main class="result-stage" id="main-content" lang={locale}>
  <Container style="--container-max: 34rem; --container-pad: var(--bb-space-4)">
    <Card style="--card-pad: clamp(var(--bb-space-4), 5vw, var(--bb-space-7))">
      <PageHead compact>
        <bb-fit><em>{title}</em></bb-fit>
      </PageHead>
      <AlertBanner tone={ok ? 'success' : 'danger'} role={ok ? 'status' : 'alert'} strong flush>
        <span class="result-copy"><bb-fit mode="block">{body}</bb-fit></span>
      </AlertBanner>
    </Card>
  </Container>
</main>

<style>
  .result-stage {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    align-items: center;
    min-block-size: 100svh;
    padding-block: var(--bb-space-4);
  }

  .result-copy {
    display: block;
    block-size: 3lh;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }
</style>
