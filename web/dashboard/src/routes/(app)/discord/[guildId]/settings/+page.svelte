<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import { getI18n } from '@bagel/kit';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { invalidateAll } from '$app/navigation';
  import { payloadOf, refusalTextOf, succeeded } from '$lib/discord/guild-draft.svelte';
  import type { TemplateItem, TemplatePlan } from '$lib/server/discord-store';

  let { data } = $props();
  const { t } = getI18n();

  let busy = $state(false);
  let disconnectOpen = $state(false);
  let disconnectForm = $state<HTMLFormElement | undefined>();
  let plan = $state<TemplatePlan | undefined>();
  let previewBusy = $state(false);
  let applyOpen = $state(false);
  let applyForm = $state<HTMLFormElement | undefined>();

  const creates = $derived((plan?.items ?? []).filter((i) => i.action === 'create'));
  const reuses = $derived((plan?.items ?? []).filter((i) => i.action !== 'create'));
  const createdRoles = $derived(creates.filter((i) => i.kind === 'role'));
  const createdChannels = $derived(creates.filter((i) => i.kind === 'channel'));

  const previewSubmit: SubmitFunction = () => {
    previewBusy = true;
    return async ({ result }) => {
      previewBusy = false;
      const p = payloadOf(result) as (ReturnType<typeof payloadOf> & { plan?: TemplatePlan }) | undefined;
      if (succeeded(result, p) && p?.plan) {
        plan = p.plan;
        return;
      }
      toast('danger', refusalTextOf(t, p, t('discord.toast.previewFailed')));
    };
  };

  const setupSubmit: SubmitFunction = () => {
    busy = true;
    return async ({ result }) => {
      busy = false;
      const p = payloadOf(result);
      if (succeeded(result, p)) {
        plan = undefined;
        toast(p?.refused ? 'info' : 'success', p?.refused ? t('discord.toast.refused') : t('discord.toast.setup'));
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
  <Heading level={6} as="h2" variant="title" id="dc-setup-h" class="block-title">{t('discord.settings.templateTitle')}</Heading>
  <Card>
    <Text size="sm" tone="muted" class="hint">{t('discord.settings.templateHelp')}</Text>
    <div class="row">
      <form method="POST" action="?/preview" use:enhance={previewSubmit}>
        <Button variant="secondary" type="submit" busy={previewBusy}>{t('discord.settings.previewCta')}</Button>
      </form>
      <Button onclick={() => (applyOpen = true)} disabled={creates.length === 0 || busy}>
        {t('discord.settings.applyCta')}
      </Button>
    </div>

    {#if plan}
      <Text size="sm" tone="muted" class="hint plan-count">
        {t('discord.settings.planCounts', { create: creates.length, reuse: reuses.length })}
      </Text>
      {#if creates.length === 0}
        <Text size="sm" class="hint">{t('discord.settings.planComplete')}</Text>
      {:else}
        {@render group(t('discord.settings.createHeading'), creates)}
      {/if}
      {#if reuses.length > 0}
        {@render group(t('discord.settings.reuseHeading'), reuses)}
      {/if}
    {/if}

    {#if !data.found}
      <Text size="sm" tone="muted" class="hint first-run">{t('discord.settings.setupFirstRun')}</Text>
    {/if}
  </Card>
</section>

{#snippet group(heading: string, items: TemplateItem[])}
  <p class="group">{heading}</p>
  <FactList>
    {#each items as item (item.kind + item.name)}
      <Fact term={item.name}>
        {t(item.kind === 'role' ? 'discord.settings.kindRole' : 'discord.settings.kindChannel')}
        {#if item.parent}, {item.parent}{/if}
        {#if item.action === 'pinned'}<Tag>{t('discord.settings.pinnedTag')}</Tag>{/if}
      </Fact>
    {/each}
  </FactList>
{/snippet}

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

<form method="POST" action="?/setup" use:enhance={setupSubmit} bind:this={applyForm} hidden></form>

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

<ConfirmDialog
  open={applyOpen}
  title={t('discord.settings.applyTitle')}
  body={t('discord.settings.applyBody', { roles: createdRoles.length, channels: createdChannels.length })}
  confirmLabel={t('discord.settings.applyCta')}
  cancelLabel={t('common.cancel')}
  {busy}
  onConfirm={() => {
    applyOpen = false;
    applyForm?.requestSubmit();
  }}
  onCancel={() => (applyOpen = false)}
/>
