<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import {
    AlertBanner,
    Button,
    Checkbox,
    Input,
    SegmentedControl,
    Text,
    Textarea,
    alertOn,
    encodeIdList,
    getI18n,
    normalizeHex,
    parseIdList,
    ticketOpenLimitN,
    ticketPanelSpec,
    LIVE_COLOR_HEX,
    TICKET_PANEL_BODY_MAX,
    TICKET_PANEL_BUTTON_MAX,
    TICKET_PANEL_DEFAULTS,
    TICKET_PANEL_TITLE_MAX
  } from '@bagel/kit';
  import GuildForm from '$lib/components/discord/GuildForm.svelte';
  import ChannelPicker from '$lib/components/discord/ChannelPicker.svelte';
  import FieldNote from '$lib/components/discord/FieldNote.svelte';
  import SwitchRow from '$lib/components/discord/SwitchRow.svelte';
  import { createGuildDraft } from '$lib/discord/guild-draft.svelte';
  import { TICKET_FIELDS } from '$lib/discord/guild-fields';
  import { categoriesOf, layoutDownOf, rolesOf, textChannelsOf } from '$lib/discord/guild-view';
  import DiscordEmbedPreview from '../DiscordEmbedPreview.svelte';

  let { data } = $props();
  const { t } = getI18n();

  const draft = createGuildDraft({ data: () => data, fields: TICKET_FIELDS, t });

  const textChannels = $derived(textChannelsOf(data.layout));
  const categories = $derived(categoriesOf(data.layout));
  const roles = $derived(rolesOf(data.layout));
  const layoutDown = $derived(layoutDownOf(data.layout));

  const LIMIT_OPTIONS = ['1', '2', '3', '4', '5'] as const;
  const staffSelected = $derived(parseIdList(draft.config.ticketStaffRoleIds));

  function toggleStaffRole(id: string, on: boolean) {
    const next = on ? [...staffSelected, id] : staffSelected.filter((r) => r !== id);
    draft.set('ticketStaffRoleIds', encodeIdList(next));
  }

  const panel = $derived(ticketPanelSpec(draft.config));
  const SWATCHES = [LIVE_COLOR_HEX, '#52b788', '#5865f2', '#dfe4e9', '#b05a46', '#8a7cc9'] as const;
  const panelColor = $derived(normalizeHex(draft.config.ticketPanelColor));
  const ticketsOn = $derived(alertOn(draft.config.ticketsEnabled));

  const repostSubmit = $derived(draft.actionSubmit(t('discord.toastReposted'), t('discord.toastRepostFailed')));
</script>

{#if layoutDown}
  <AlertBanner variant="warn">{t('discord.layoutUnavailable')}</AlertBanner>
{/if}

<GuildForm {draft} id="dc-tickets-h" title={t('discord.ticketsTitle')} hint={t('discord.ticketsSectionHelp')}>
  <SwitchRow
    {draft}
    invalid={draft.invalid}
    field="ticketsEnabled"
    label={t('discord.ticketsLabel')}
    help={t('discord.ticketsHelp')}
    defaultOn
  />

  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="ticketChannelId"
    label={t('discord.ticketChannelLabel')}
    help={t('discord.ticketChannelHelp')}
    options={textChannels}
    prefix="#"
  />
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="ticketCategoryId"
    label={t('discord.ticketCategoryLabel')}
    help={t('discord.ticketCategoryHelp')}
    options={categories}
  />
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="ticketArchiveCategoryId"
    label={t('discord.ticketArchiveLabel')}
    help={t('discord.ticketArchiveHelp')}
    options={categories}
  />
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="ticketLogChannelId"
    label={t('discord.ticketLogLabel')}
    help={t('discord.ticketLogHelp')}
    options={textChannels}
    prefix="#"
  />

  <fieldset class="setting-row stacked staff">
    <legend><Text as="span" size="sm">{t('discord.staffRolesLabel')}</Text></legend>
    <Text as="span" size="xs" tone="muted" id="dch-staff">{t('discord.staffRolesHelp')}</Text>
    {#if roles.length === 0}
      <Text as="span" size="xs" tone="muted">{t('discord.staffRolesEmpty')}</Text>
    {:else}
      <div class="checks">
        {#each roles as role (role.id)}
          <Checkbox
            class="check"
            aria-describedby="dch-staff"
            bind:checked={() => staffSelected.includes(role.id), (on) => toggleStaffRole(role.id, on)}
          ><Text as="span" size="sm" truncate>@{role.name}</Text></Checkbox>
        {/each}
      </div>
    {/if}
    <FieldNote invalid={draft.invalid} field="ticketStaffRoleIds" />
  </fieldset>

  <div class="setting-row">
    <span class="tr-text">
      <Text as="span" size="sm">{t('discord.openLimitLabel')}</Text>
      <Text as="span" size="xs" tone="muted">{t('discord.openLimitHelp')}</Text>
    </span>
    <SegmentedControl
      options={LIMIT_OPTIONS}
      label={t('discord.openLimitLabel')}
      bind:value={() => String(ticketOpenLimitN(draft.config)), (v) => draft.set('ticketOpenLimit', v)}
    />
    <FieldNote invalid={draft.invalid} field="ticketOpenLimit" />
  </div>

  <SwitchRow
    {draft}
    invalid={draft.invalid}
    field="ticketTranscriptEnabled"
    label={t('discord.transcriptLabel')}
    help={t('discord.transcriptHelp')}
    defaultOn
  />

  <h3 class="group">{t('discord.panelTitle')}</h3>
  <Text size="sm" tone="muted" class="hint">{t('discord.panelHelp')}</Text>

  <div class="setting-row">
    <label class="tr-text" for="dc-panel-title">
      <Text as="span" size="sm">{t('discord.panelTitleLabel')}</Text>
      <Text as="span" size="xs" tone="muted">{TICKET_PANEL_DEFAULTS.title}</Text>
    </label>
    <Input
      id="dc-panel-title"
      maxlength={TICKET_PANEL_TITLE_MAX}
      placeholder={TICKET_PANEL_DEFAULTS.title}
      value={draft.config.ticketPanelTitle}
      oninput={(e: Event & { currentTarget: HTMLInputElement }) => draft.set('ticketPanelTitle', e.currentTarget.value)}
    />
    <FieldNote invalid={draft.invalid} field="ticketPanelTitle" />
  </div>

  <div class="setting-row stacked">
    <label class="tr-text" for="dc-panel-body">
      <Text as="span" size="sm">{t('discord.panelBodyLabel')}</Text>
      <Text as="span" size="xs" tone="muted">{TICKET_PANEL_DEFAULTS.body}</Text>
    </label>
    <Textarea
      id="dc-panel-body"
      rows={4}
      maxlength={TICKET_PANEL_BODY_MAX}
      placeholder={TICKET_PANEL_DEFAULTS.body}
      value={draft.config.ticketPanelBody}
      oninput={(e: Event & { currentTarget: HTMLTextAreaElement }) => draft.set('ticketPanelBody', e.currentTarget.value)}
    />
    <FieldNote invalid={draft.invalid} field="ticketPanelBody" />
  </div>

  <div class="setting-row">
    <label class="tr-text" for="dc-panel-button">
      <Text as="span" size="sm">{t('discord.panelButtonLabel')}</Text>
      <Text as="span" size="xs" tone="muted">{TICKET_PANEL_DEFAULTS.button}</Text>
    </label>
    <Input
      id="dc-panel-button"
      maxlength={TICKET_PANEL_BUTTON_MAX}
      placeholder={TICKET_PANEL_DEFAULTS.button}
      value={draft.config.ticketPanelButton}
      oninput={(e: Event & { currentTarget: HTMLInputElement }) => draft.set('ticketPanelButton', e.currentTarget.value)}
    />
    <FieldNote invalid={draft.invalid} field="ticketPanelButton" />
  </div>

  <div class="setting-row">
    <label class="tr-text" for="dc-panel-color">
      <Text as="span" size="sm">{t('discord.panelColorLabel')}</Text>
      <Text as="span" size="xs" tone="muted">{t('discord.panelColorHelp')}</Text>
    </label>
    <span class="colors">
      {#each SWATCHES as swatch (swatch)}
        <button
          type="button"
          class="swatch {panelColor === swatch ? 'on' : ''}"
          style="background: {swatch}"
          aria-label={t('discord.swatchLabel', { hex: swatch })}
          aria-pressed={panelColor === swatch}
          onclick={() => draft.set('ticketPanelColor', swatch)}
        ></button>
      {/each}
      <Input
        id="dc-panel-color"
        type="color"
        value={panelColor}
        oninput={(e: Event & { currentTarget: HTMLInputElement }) => draft.set('ticketPanelColor', normalizeHex(e.currentTarget.value))}
      />
    </span>
    <FieldNote invalid={draft.invalid} field="ticketPanelColor" />
  </div>

  <DiscordEmbedPreview
    caption={t('discord.panelPreviewCaption')}
    title={panel.title}
    body={panel.body}
    button={panel.button}
    color={panel.color}
  />

  {#snippet after()}
    <div class="repost">
      <Text size="sm" tone="muted" class="hint">{t('discord.repostHelp')}</Text>
      <form method="POST" action="?/repost" use:enhance={repostSubmit}>
        <Button variant="secondary" type="submit" loading={draft.busy} disabled={!ticketsOn}>
          {t('discord.repostCta')}
        </Button>
      </form>
    </div>
  {/snippet}
</GuildForm>
