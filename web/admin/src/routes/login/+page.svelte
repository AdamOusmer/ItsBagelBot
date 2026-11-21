<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { page } from '$app/state';
  import PublicShell from '@bagel/kit/components/PublicShell.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { SITE } from '@bagel/kit/site-links';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Container from '@bagel/ui/svelte/Container.svelte';
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import SkipLink from '@bagel/ui/svelte/SkipLink.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';

  const { locale, t } = getI18n();
  const notices = new Map([
    ['denied', 'admin.login.errDenied'],
    ['state', 'admin.login.errState'],
    ['oauth', 'admin.login.errOauth'],
    ['scope', 'admin.login.errScope']
  ]);

  const consent = t('admin.login.consent').split(/(\{terms\}|\{privacy\})/);
  const consentLinks = new Map([
    ['{terms}', { href: `${SITE.web}/terms`, label: t('admin.login.terms') }],
    ['{privacy}', { href: `${SITE.web}/privacy`, label: t('admin.login.privacy') }]
  ]);

  const notice = $derived(notices.get(page.url.searchParams.get('e') ?? ''));
  const next = $derived(page.url.searchParams.get('next'));
  const loginHref = $derived(next ? `/auth/login?next=${encodeURIComponent(next)}` : '/auth/login');
</script>

<svelte:head>
  <title>{t('admin.login.sub')} · ItsBagelBot</title>
</svelte:head>

<div lang={locale}>
  <SkipLink href="#main-content" />
  <PublicShell loginPath="/auth/login" footer={false}>
    <main class="login-stage" id="main-content" tabindex="-1">
      <Container style="--container-max: 34rem; --container-pad: var(--bb-space-4)">
        <Card style="--card-pad: clamp(var(--bb-space-4), 5vw, var(--bb-space-7))">
          <PageHead compact>
            <bb-fit><em>{t('admin.login.sub')}</em></bb-fit>
          </PageHead>

          <Stack gap={4}>
            <Text tone="muted">
              <span class="login-copy"><bb-fit mode="block">{t('admin.login.lede')}</bb-fit></span>
            </Text>

            <Grid gap={2} min="12rem">
              <ButtonLink href={loginHref} variant="twitch" block data-sveltekit-reload aria-describedby={notice ? 'signin-notice' : undefined}>
                <bb-fit>{t('admin.login.cta')}</bb-fit>
              </ButtonLink>
              <ButtonLink href={SITE.web} variant="secondary" block>
                <bb-fit>{t('login.back')}</bb-fit>
              </ButtonLink>
            </Grid>

            <Text size="sm" tone="muted">
              <span class="login-copy">
                <bb-fit mode="block">
                  {#each consent as part, index (index)}
                    {@const link = consentLinks.get(part)}
                    {#if link}<TextLink {...link} variant="inline" external />{:else}{part}{/if}
                  {/each}
                </bb-fit>
              </span>
            </Text>
          </Stack>
        </Card>

        <div class="login-notice" class:visible={notice} inert={!notice} aria-hidden={!notice}>
          <AlertBanner id="signin-notice" tone="danger" strong flush>
            <span class="login-copy"><bb-fit mode="block">{notice ? t(notice) : ''}</bb-fit></span>
          </AlertBanner>
        </div>
      </Container>
    </main>
  </PublicShell>
</div>

<style>
  .login-stage {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    align-items: center;
    min-block-size: 100svh;
    padding-block: calc(var(--bb-nav-height) + env(safe-area-inset-top, 0px) + var(--bb-space-4)) var(--bb-space-4);
  }

  .login-copy {
    display: block;
    block-size: 2lh;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .login-notice {
    margin-block-start: var(--bb-space-3);
    opacity: 0;
    transition: opacity var(--bb-dur-fast) ease;
  }

  .login-notice.visible {
    opacity: 1;
  }

  @media (prefers-reduced-motion: reduce) {
    .login-notice {
      transition: none;
    }
  }
</style>
