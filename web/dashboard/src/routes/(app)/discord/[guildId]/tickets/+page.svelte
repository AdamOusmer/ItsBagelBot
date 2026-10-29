<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import {
    getI18n,
    ticketPanelSpec,
    TICKET_PANEL_BODY_MAX,
    TICKET_PANEL_BUTTON_MAX,
    TICKET_PANEL_TITLE_MAX
  } from '@bagel/kit';
  import ModuleFields from '#lib/components/discord/ModuleFields.svelte';
  import ModuleShell from '#lib/components/discord/ModuleShell.svelte';
  import { fieldLabelOf } from '#lib/discord/action-result';
  import { createModuleDraft } from '#lib/discord/module-draft.svelte';
  import { layoutDownOf } from '#lib/discord/guild-view';
  import DiscordEmbedPreview from '../DiscordEmbedPreview.svelte';

  let { data } = $props();
  const { t } = getI18n();

  const draft = createModuleDraft({ data: () => data }, t);

  const panel = $derived(ticketPanelSpec(draft.config));
  const repostSubmit = $derived(draft.actionSubmit(t('discord.toast.reposted'), t('discord.toast.repostFailed')));
  const limits = $derived([
    { label: fieldLabelOf(t, 'tickets', 'ticketPanelTitle'), n: draft.config.ticketPanelTitle.length, max: TICKET_PANEL_TITLE_MAX },
    { label: fieldLabelOf(t, 'tickets', 'ticketPanelBody'), n: draft.config.ticketPanelBody.length, max: TICKET_PANEL_BODY_MAX },
    { label: fieldLabelOf(t, 'tickets', 'ticketPanelButton'), n: draft.config.ticketPanelButton.length, max: TICKET_PANEL_BUTTON_MAX }
  ]);
</script>

{#if layoutDownOf(data.layout)}
  <AlertBanner tone="warning">{t('discord.layoutUnavailable')}</AlertBanner>
{/if}

<ModuleShell {draft} guildId={data.guildId}>
  <ModuleFields {draft} layout={data.layout}>
    {#snippet afterField(key)}
      {#if key === 'ticketPanelButton'}
        <DiscordEmbedPreview
          caption={t('discord.tickets.panelPreviewCaption')}
          title={panel.title}
          body={panel.body}
          button={panel.button}
          color={panel.color}
          {limits}
        />
      {/if}
    {/snippet}
  </ModuleFields>

  {#snippet footer()}
    <div class="repost">
      <Text size="sm" tone="muted" class="hint">{t('discord.tickets.repostHelp')}</Text>
      <form method="POST" action="?/repost" use:enhance={repostSubmit}>
        <Button variant="secondary" type="submit" busy={draft.busy} disabled={!draft.enabled}>
          {t('discord.tickets.repostCta')}
        </Button>
      </form>
    </div>
  {/snippet}
</ModuleShell>
