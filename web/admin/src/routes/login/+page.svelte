<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { page } from '$app/state';
  import AuroraBg from '@bagel/ui/svelte/AuroraBg.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import LightField from '@bagel/ui/svelte/LightField.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';

  const { t } = getI18n();

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
    <Card glass>
      <Stack gap={5}>
        <Stack gap={4} align="center">
          <img class="logo" src="/logo.png" alt={t('common.appName')} width="44" height="44" />
          <Stack gap={1} align="center">
            <Heading level={4} as="h1">{t('common.appName')}</Heading>
            <Label mono as="span">{t('admin.login.sub')}</Label>
          </Stack>
          {#if notice}<Text size="xs" tone="danger" mono>{notice}</Text>{/if}
        </Stack>
        <Text size="sm" tone="muted">{t('admin.login.lede')}</Text>
        <Stack gap={2} align="center">
          <div class="twitch">
            <ButtonLink href="/auth/login" variant="brand" block>
              <Icon name="twitch" />
              {t('admin.login.cta')}
            </ButtonLink>
          </div>
          <ButtonLink href="/auth/bot/login" variant="ghost">{t('admin.login.botCta')}</ButtonLink>
        </Stack>
      </Stack>
    </Card>
  </div>
</main>

<style>
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
    min-height: 100svh;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: var(--bb-space-5);
  }
  .panel {
    --card-pad: 44px 40px 40px;
    width: 100%;
    max-width: 420px;
    text-align: center;
  }
  .logo {
    border-radius: var(--bb-radius-sm);
  }
  .twitch {
    --btn-brand: #9146ff;
    --btn-brand-hover: #7d2ff5;
    --btn-brand-glow: rgba(145, 70, 255, 0.35);
    width: 100%;
  }
</style>
