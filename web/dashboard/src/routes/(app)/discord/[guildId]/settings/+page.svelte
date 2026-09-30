<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import { Button, ButtonLink, Card, ConfirmDialog, Heading, Text, toast } from '@bagel/ui/svelte';
  import { getI18n } from '@bagel/kit';
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
        toast(p?.refused ? 'danger' : 'success', p?.refused ? t('discord.toast.refused') : t('discord.toast.setup'));
        await invalidateAll();
        return;
      }
      toast('danger', refusalTextOf(t, p, t('discord.toast.setupFailed')));
    };
  };

  const disconnectSubmit: SubmitFunction = () => {
    busy = true;
    return async ({ result }) => {
      busy = false;
      const p = payloadOf(result);
      if (succeeded(result, p)) return;
      toast('danger', refusalTextOf(t, p, t('discord.toast.disconnectFailed')));
    };
  };
</script>

<section class="block reveal" style="--i:1" aria-labelledby="dc-setup-h">
  <Heading level={6} as="h2" variant="title" id="dc-setup-h" class="block-title">{t('discord.settings.setupTitle')}</Heading>
  <Card>
    <Text size="sm" tone="muted" class="hint">{t('discord.settings.setupHelp')}</Text>
    <form method="POST" action="?/setup" use:enhance={setupSubmit}>
      <Button variant="secondary" type="submit" busy={busy}>{t('discord.settings.setupCta')}</Button>
    </form>

    {#if !data.found}
      <Text size="sm" tone="muted" class="hint first-run">{t('discord.settings.setupFirstRun')}</Text>
    {/if}
  </Card>
</section>

<section class="block reveal" style="--i:2" aria-labelledby="dc-danger-h">
  <Heading level={6} as="h2" variant="title" id="dc-danger-h" class="block-title">{t('discord.settings.dangerTitle')}</Heading>
  <Card>
    <Text size="sm" tone="muted" class="hint">{t('discord.settings.disconnectBody')}</Text>
    <div class="row">
      <ButtonLink variant="ghost" href="/discord/connect" data-sveltekit-reload>
        {t('discord.settings.reconnectCta')}
      </ButtonLink>
      <Button onclick={() => (disconnectOpen = true)} tone="danger">
        {t('discord.settings.disconnectCta')}
      </Button>
    </div>
  </Card>
</section>

<form method="POST" action="?/disconnect" use:enhance={disconnectSubmit} bind:this={disconnectForm} hidden></form>

<ConfirmDialog
  open={disconnectOpen}
  title={t('discord.settings.disconnectTitle')}
  body={t('discord.settings.disconnectBody')}
  confirmLabel={t('discord.settings.disconnectCta')}
  cancelLabel={t('common.cancel')}
  {busy}
  onConfirm={() => {
    disconnectOpen = false;
    disconnectForm?.requestSubmit();
  }}
  onCancel={() => (disconnectOpen = false)}
  tone="danger"
/>

