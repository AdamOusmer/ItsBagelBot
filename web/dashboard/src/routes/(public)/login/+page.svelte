<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import LoginHero from '@bagel/kit/components/LoginHero.svelte';
  import { getI18n } from '@bagel/kit';
  import PublicHead from '#lib/components/public/PublicHead.svelte';

  const { t } = getI18n();

  const notices = $derived({
    signedout: t('login.noticeSignedout'),
    revoked: t('login.noticeRevoked'),
    banned: t('login.noticeBanned'),
    link: t('login.noticeLink'),
    retry: t('login.noticeRetry'),
    imp: t('login.noticeImpersonation'),
    declined: t('login.noticeDeclined'),
    state: t('login.noticeState'),
    oauth: t('login.noticeOauth'),
    bot: t('login.noticeBot'),
    scope: t('login.noticeScope')
  });
</script>

<PublicHead title={t('login.metaTitle')} description={t('login.metaDescription')} url="https://dashboard.itsbagelbot.com/login" />

<LoginHero {notices} />
