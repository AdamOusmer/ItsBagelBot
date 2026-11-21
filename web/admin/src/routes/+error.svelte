<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { page } from '$app/state';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Container from '@bagel/ui/svelte/Container.svelte';
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';

  const { locale, t } = getI18n();

  function errorView(status: number) {
    switch (status) {
      case 404:
        return {
          title: t('admin.error.notFoundTitle'),
          body: t('admin.error.notFoundBody'),
          primary: t('admin.error.backHome'),
          href: '/'
        };
      case 401:
      case 403:
        return {
          title: t('admin.error.accessTitle'),
          body: t('admin.error.accessBody'),
          primary: t('error.signIn'),
          href: '/login'
        };
      case 500:
      case 503:
        return {
          title: t('admin.error.serverTitle'),
          body: t('admin.error.serverBody'),
          primary: t('error.tryAgain')
        };
      default:
        return {
          title: t('admin.error.unexpectedTitle'),
          body: page.error?.message ?? t('admin.error.unexpectedBody'),
          primary: t('error.tryAgain')
        };
    }
  }

  function goBack() {
    if (history.length > 1) history.back();
    else location.assign('/');
  }

  const view = $derived(errorView(page.status));
</script>

<svelte:head>
  <title>{page.status} · ItsBagelBot {t('error.appAdmin')}</title>
</svelte:head>

<main class="error-stage" id="main-content" lang={locale}>
  <Container style="--container-max: 34rem; --container-pad: var(--bb-space-4)">
    <Card style="--card-pad: clamp(var(--bb-space-4), 5vw, var(--bb-space-7))">
      <PageHead compact>
        <bb-fit><span class="error-code">{page.status}{' '}</span><em>{view.title}</em></bb-fit>
      </PageHead>

      <Stack gap={4}>
        <Text tone="muted">
          <span class="error-copy"><bb-fit mode="block">{view.body}</bb-fit></span>
        </Text>

        <Grid gap={2} min="12rem">
          {#if view.href}
            <ButtonLink href={view.href} block><bb-fit>{view.primary}</bb-fit></ButtonLink>
          {:else}
            <Button block onclick={() => location.reload()}><bb-fit>{view.primary}</bb-fit></Button>
          {/if}
          <Button variant="secondary" block onclick={goBack}><bb-fit>{t('error.goBack')}</bb-fit></Button>
        </Grid>
      </Stack>
    </Card>
  </Container>
</main>

<style>
  .error-stage {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    align-items: center;
    min-block-size: 100svh;
    padding-block: var(--bb-space-4);
  }

  .error-copy {
    display: block;
    block-size: 3lh;
    overflow-wrap: anywhere;
  }

  .error-code {
    margin-inline-end: 0.3em;
  }
</style>
