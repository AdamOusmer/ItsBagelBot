<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The two buttons that change the server rather than a setting: re-run the
  // fill, and unbind it.
  //
  // No draft here, so no dirty guard: nothing on this page is editable. That is
  // also why disconnect is safe to reach from it -- the guard that would have
  // to stand down for the redirect belongs to the sub-pages that have a draft,
  // and none of them is mounted while this one is.
  import { enhance } from '$app/forms';
  import { Button, ButtonLink, Card, ConfirmDialog, getI18n, toast } from '@bagel/shared';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { invalidateAll } from '$app/navigation';
  import { payloadOf, refusalTextOf, succeeded } from '$lib/discord/guild-draft.svelte';

  let { data } = $props();
  const { t } = getI18n();

  let busy = $state(false);
  let disconnectOpen = $state(false);
  let disconnectForm = $state<HTMLFormElement | undefined>();

  const setupSubmit: SubmitFunction = () => {
    busy = true;
    return async ({ result }) => {
      busy = false;
      const p = payloadOf(result);
      if (succeeded(result, p)) {
        // `refused` is setup reporting it did NOT rebuild: the guild already
        // had a layout, so the fill adopted what it recognised and left the
        // rest. That is a warning, not a success, or the streamer waits for
        // channels that are never coming.
        toast(p?.refused ? 'err' : 'ok', p?.refused ? t('discord.toastRefused') : t('discord.toastSetup'));
        await invalidateAll();
        return;
      }
      toast('err', refusalTextOf(t, p, t('discord.toastSetupFailed')));
    };
  };

  const disconnectSubmit: SubmitFunction = () => {
    busy = true;
    return async ({ result }) => {
      busy = false;
      const p = payloadOf(result);
      // A successful disconnect never gets here: the action throws a redirect
      // to /discord, which enhance follows.
      if (succeeded(result, p)) return;
      toast('err', refusalTextOf(t, p, t('discord.toastDisconnectFailed')));
    };
  };
</script>

<section class="block reveal" style="--i:1" aria-labelledby="dc-setup-h">
  <h2 id="dc-setup-h" class="block-title">{t('discord.setupTitle')}</h2>
  <Card>
    <p class="hint">{t('discord.setupHelp')}</p>
    <form method="POST" action="?/setup" use:enhance={setupSubmit}>
      <Button variant="secondary" type="submit" icon="server" loading={busy}>{t('discord.setupCta')}</Button>
    </form>

    <!-- Never saved: the guild is bound but has no config row, so nothing Bagel
         does in it has been set up yet. The nudge is here rather than on the
         overview because this is the button that fixes it. -->
    {#if !data.found}
      <p class="hint first-run">{t('discord.setupFirstRun')}</p>
    {/if}
  </Card>
</section>

<section class="block reveal" style="--i:2" aria-labelledby="dc-danger-h">
  <h2 id="dc-danger-h" class="block-title">{t('discord.dangerTitle')}</h2>
  <Card>
    <p class="hint">{t('discord.disconnectBody')}</p>
    <div class="row">
      <ButtonLink variant="ghost" icon="power" href="/discord/connect" data-sveltekit-reload>
        {t('discord.reconnectCta')}
      </ButtonLink>
      <Button variant="destructive" icon="ban" onclick={() => (disconnectOpen = true)}>
        {t('discord.disconnectCta')}
      </Button>
    </div>
  </Card>
</section>

<form method="POST" action="?/disconnect" use:enhance={disconnectSubmit} bind:this={disconnectForm} hidden></form>

<ConfirmDialog
  open={disconnectOpen}
  title={t('discord.disconnectTitle')}
  body={t('discord.disconnectBody')}
  confirmLabel={t('discord.disconnectCta')}
  cancelLabel={t('common.cancel')}
  danger
  {busy}
  onConfirm={() => {
    disconnectOpen = false;
    disconnectForm?.requestSubmit();
  }}
  onCancel={() => (disconnectOpen = false)}
/>

<style>
  .first-run {
    margin: 14px 0 0;
  }
</style>
