<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Button from '@bagel/ui/svelte/Button.svelte';
  import { getI18n } from '../lib/i18n/context';

  const { t } = getI18n();

  let { menu = false }: { menu?: boolean } = $props();

  function clearOnboarding() {
    try {
      localStorage.removeItem('bb-onboarded');
    } catch {
      return;
    }
  }
</script>

<form method="POST" action="/auth/logout" onsubmit={clearOnboarding}>
  {#if menu}
    <button type="submit" class="bb-profile-topbar__op-menu-item" role="menuitem">{t('topbar.logout')}</button>
  {:else}
    <Button variant="ghost" type="submit" style="width:100%;justify-content:center;margin-top:10px">
      {t('topbar.logout')}
    </Button>
  {/if}
</form>
