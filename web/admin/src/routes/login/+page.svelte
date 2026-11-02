<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import LoginHero from '@bagel/kit/components/LoginHero.svelte';
  import PublicShell from '@bagel/kit/components/PublicShell.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';

  const { t } = getI18n();

  const notices = $derived({
    denied: t('admin.login.errDenied'),
    state: t('admin.login.errState'),
    oauth: t('admin.login.errOauth'),
    scope: t('admin.login.errScope')
  });
</script>

<PublicShell loginPath="/auth/login" footer={false}>
  <LoginHero {notices} />
</PublicShell>
