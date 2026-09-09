<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The ticket desk: the switch, its channels and staff, the panel embed and a
  // live preview of it. The desk keeps its own channel pickers rather than
  // sending the streamer to the Channels page, because the four of them are
  // meaningless outside tickets and picking them is part of setting the desk up.
  import { enhance } from '$app/forms';
  import {
    AlertBanner,
    Button,
    SegmentedControl,
    Switch,
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
  // Six presets so the common case is one click and the picker stays for the
  // rest; the first is the colour every other Bagel embed already uses.
  const SWATCHES = [LIVE_COLOR_HEX, '#52b788', '#5865f2', '#dfe4e9', '#b05a46', '#8a7cc9'] as const;
  const panelColor = $derived(normalizeHex(draft.config.ticketPanelColor));
  const ticketsOn = $derived(alertOn(draft.config.ticketsEnabled));

  const repostSubmit = $derived(draft.actionSubmit(t('discord.toastReposted'), t('discord.toastRepostFailed')));
</script>

{#if layoutDown}
  <AlertBanner variant="warn">{t('discord.layoutUnavailable')}</AlertBanner>
{/if}

<GuildForm {draft} id="dc-tickets-h" title={t('discord.ticketsTitle')} hint={t('discord.ticketsSectionHelp')}>
  <div class="setting-row">
    <span class="tr-text">
      <span class="tr-label">{t('discord.ticketsLabel')}</span>
      <span class="tr-help" id="dcs-tickets">{t('discord.ticketsHelp')}</span>
    </span>
    <Switch
      label={t('discord.ticketsLabel')}
      describedby="dcs-tickets"
      checked={ticketsOn}
      onchange={(v) => draft.setFlag('ticketsEnabled', v)}
    />
    <FieldNote invalid={draft.invalid} field="ticketsEnabled" />
  </div>

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
    <legend class="tr-label">{t('discord.staffRolesLabel')}</legend>
    <span class="tr-help" id="dch-staff">{t('discord.staffRolesHelp')}</span>
    {#if roles.length === 0}
      <span class="tr-help">{t('discord.staffRolesEmpty')}</span>
    {:else}
      <div class="checks">
        {#each roles as role (role.id)}
          <label class="check">
            <input
              type="checkbox"
              aria-describedby="dch-staff"
              checked={staffSelected.includes(role.id)}
              onchange={(e) => toggleStaffRole(role.id, e.currentTarget.checked)}
            />
            <span>@{role.name}</span>
          </label>
        {/each}
      </div>
    {/if}
    <FieldNote invalid={draft.invalid} field="ticketStaffRoleIds" />
  </fieldset>

  <div class="setting-row">
    <span class="tr-text">
      <span class="tr-label">{t('discord.openLimitLabel')}</span>
      <span class="tr-help">{t('discord.openLimitHelp')}</span>
    </span>
    <SegmentedControl
      options={LIMIT_OPTIONS}
      label={t('discord.openLimitLabel')}
      bind:value={() => String(ticketOpenLimitN(draft.config)), (v) => draft.set('ticketOpenLimit', v)}
    />
    <FieldNote invalid={draft.invalid} field="ticketOpenLimit" />
  </div>

  <div class="setting-row">
    <span class="tr-text">
      <span class="tr-label">{t('discord.transcriptLabel')}</span>
      <span class="tr-help" id="dcs-transcript">{t('discord.transcriptHelp')}</span>
    </span>
    <Switch
      label={t('discord.transcriptLabel')}
      describedby="dcs-transcript"
      checked={alertOn(draft.config.ticketTranscriptEnabled)}
      onchange={(v) => draft.setFlag('ticketTranscriptEnabled', v)}
    />
    <FieldNote invalid={draft.invalid} field="ticketTranscriptEnabled" />
  </div>

  <h3 class="group">{t('discord.panelTitle')}</h3>
  <p class="hint">{t('discord.panelHelp')}</p>

  <div class="setting-row">
    <label class="tr-text" for="dc-panel-title">
      <span class="tr-label">{t('discord.panelTitleLabel')}</span>
      <span class="tr-help">{TICKET_PANEL_DEFAULTS.title}</span>
    </label>
    <input
      id="dc-panel-title"
      class="setting-input"
      maxlength={TICKET_PANEL_TITLE_MAX}
      placeholder={TICKET_PANEL_DEFAULTS.title}
      value={draft.config.ticketPanelTitle}
      oninput={(e) => draft.set('ticketPanelTitle', e.currentTarget.value)}
    />
    <FieldNote invalid={draft.invalid} field="ticketPanelTitle" />
  </div>

  <div class="setting-row stacked">
    <label class="tr-text" for="dc-panel-body">
      <span class="tr-label">{t('discord.panelBodyLabel')}</span>
      <span class="tr-help">{TICKET_PANEL_DEFAULTS.body}</span>
    </label>
    <textarea
      id="dc-panel-body"
      class="setting-input setting-textarea"
      maxlength={TICKET_PANEL_BODY_MAX}
      placeholder={TICKET_PANEL_DEFAULTS.body}
      value={draft.config.ticketPanelBody}
      oninput={(e) => draft.set('ticketPanelBody', e.currentTarget.value)}
    ></textarea>
    <FieldNote invalid={draft.invalid} field="ticketPanelBody" />
  </div>

  <div class="setting-row">
    <label class="tr-text" for="dc-panel-button">
      <span class="tr-label">{t('discord.panelButtonLabel')}</span>
      <span class="tr-help">{TICKET_PANEL_DEFAULTS.button}</span>
    </label>
    <input
      id="dc-panel-button"
      class="setting-input"
      maxlength={TICKET_PANEL_BUTTON_MAX}
      placeholder={TICKET_PANEL_DEFAULTS.button}
      value={draft.config.ticketPanelButton}
      oninput={(e) => draft.set('ticketPanelButton', e.currentTarget.value)}
    />
    <FieldNote invalid={draft.invalid} field="ticketPanelButton" />
  </div>

  <div class="setting-row">
    <label class="tr-text" for="dc-panel-color">
      <span class="tr-label">{t('discord.panelColorLabel')}</span>
      <span class="tr-help">{t('discord.panelColorHelp')}</span>
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
      <input
        id="dc-panel-color"
        type="color"
        class="color-input"
        value={panelColor}
        oninput={(e) => draft.set('ticketPanelColor', normalizeHex(e.currentTarget.value))}
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
      <p class="hint">{t('discord.repostHelp')}</p>
      <form method="POST" action="?/repost" use:enhance={repostSubmit}>
        <Button variant="secondary" type="submit" loading={draft.busy} disabled={!ticketsOn}>
          {t('discord.repostCta')}
        </Button>
      </form>
    </div>
  {/snippet}
</GuildForm>
