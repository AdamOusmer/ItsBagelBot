<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import {
    AlertBanner,
    Button,
    Checkbox,
    Input,
    SearchInput,
    SegmentedControl,
    Text,
    Textarea
  } from '@bagel/ui/svelte';
  import {
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

  let roleQuery = $state('');
  const seededStaff = $derived(new Set(parseIdList(data.config?.ticketStaffRoleIds ?? '')));
  const knownRoleIds = $derived(new Set(roles.map((r) => r.id)));
  const missingStaff = $derived(staffSelected.filter((id) => !knownRoleIds.has(id)));
  const visibleRoles = $derived.by(() => {
    const q = roleQuery.trim().toLowerCase();
    const hits = roles.filter((r) => !q || r.name.toLowerCase().includes(q));
    return [...hits.filter((r) => seededStaff.has(r.id)), ...hits.filter((r) => !seededStaff.has(r.id))];
  });

  function toggleStaffRole(id: string, on: boolean) {
    const next = on ? [...staffSelected, id] : staffSelected.filter((r) => r !== id);
    draft.set('ticketStaffRoleIds', encodeIdList(next));
  }

  const panel = $derived(ticketPanelSpec(draft.config));
  const SWATCHES = [LIVE_COLOR_HEX, '#52b788', '#5865f2', '#dfe4e9', '#b05a46', '#8a7cc9'] as const;
  const panelColor = $derived(normalizeHex(draft.config.ticketPanelColor));
  const ticketsOn = $derived(alertOn(draft.config.ticketsEnabled));

  const repostSubmit = $derived(draft.actionSubmit(t('discord.toast.reposted'), t('discord.toast.repostFailed')));
</script>

{#if layoutDown}
  <AlertBanner tone="warning">{t('discord.layoutUnavailable')}</AlertBanner>
{/if}

<GuildForm {draft} id="dc-tickets-h" title={t('discord.tickets.title')} hint={t('discord.tickets.sectionHelp')}>
  <SwitchRow
    {draft}
    invalid={draft.invalid}
    field="ticketsEnabled"
    label={t('discord.tickets.label')}
    help={t('discord.tickets.help')}
    defaultOn
  />

  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="ticketChannelId"
    label={t('discord.tickets.channelLabel')}
    help={t('discord.tickets.channelHelp')}
    options={textChannels}
    prefix="#"
  />
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="ticketCategoryId"
    label={t('discord.tickets.categoryLabel')}
    help={t('discord.tickets.categoryHelp')}
    options={categories}
  />
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="ticketArchiveCategoryId"
    label={t('discord.tickets.archiveLabel')}
    help={t('discord.tickets.archiveHelp')}
    options={categories}
  />
  <ChannelPicker
    {draft}
    invalid={draft.invalid}
    field="ticketLogChannelId"
    label={t('discord.tickets.logLabel')}
    help={t('discord.tickets.logHelp')}
    options={textChannels}
    prefix="#"
  />

  <fieldset class="setting-row stacked staff">
    <legend><Text as="span" size="sm">{t('discord.tickets.staffRolesLabel')}</Text></legend>
    <Text as="span" size="xs" tone="muted" id="dch-staff">{t('discord.tickets.staffRolesHelp')}</Text>
    {#if roles.length === 0}
      <Text as="span" size="xs" tone="muted">{t('discord.tickets.staffRolesEmpty')}</Text>
    {:else}
      {#if missingStaff.length}
        <Text as="span" size="xs" tone="muted" role="status">{t('discord.tickets.staffRoleMissingHelp')}</Text>
      {/if}
      {#if roles.length > 8}
        <SearchInput bind:value={roleQuery} placeholder={t('discord.tickets.staffRolesFilterPh')} aria-label={t('discord.tickets.staffRolesFilterLabel')} clearLabel={t('common.searchClear')} autocomplete="off" fill />
      {/if}
      <div class="checks">
        {#each missingStaff as id (id)}
          <Checkbox
            class="check"
            aria-describedby="dch-staff"
            bind:checked={() => true, () => toggleStaffRole(id, false)}
          ><Text as="span" size="sm" truncate>{t('discord.tickets.staffRoleMissing', { id })}</Text></Checkbox>
        {/each}
        {#each visibleRoles as role (role.id)}
          <Checkbox
            class="check"
            aria-describedby="dch-staff"
            bind:checked={() => staffSelected.includes(role.id), (on) => toggleStaffRole(role.id, on)}
          ><Text as="span" size="sm" truncate>@{role.name}</Text></Checkbox>
        {/each}
      </div>
      {#if visibleRoles.length === 0}
        <Text as="span" size="xs" tone="muted">{t('discord.tickets.staffRolesNoMatch')}</Text>
      {/if}
    {/if}
    <FieldNote invalid={draft.invalid} field="ticketStaffRoleIds" />
  </fieldset>

  <div class="setting-row">
    <span class="tr-text">
      <Text as="span" size="sm">{t('discord.tickets.openLimitLabel')}</Text>
      <Text as="span" size="xs" tone="muted">{t('discord.tickets.openLimitHelp')}</Text>
    </span>
    <SegmentedControl
      options={LIMIT_OPTIONS}
      label={t('discord.tickets.openLimitLabel')}
      bind:value={() => String(ticketOpenLimitN(draft.config)), (v) => draft.set('ticketOpenLimit', v)}
    />
    <FieldNote invalid={draft.invalid} field="ticketOpenLimit" />
  </div>

  <SwitchRow
    {draft}
    invalid={draft.invalid}
    field="ticketTranscriptEnabled"
    label={t('discord.tickets.transcriptLabel')}
    help={t('discord.tickets.transcriptHelp')}
    defaultOn
  />

  <h3 class="group">{t('discord.tickets.panelTitle')}</h3>
  <Text size="sm" tone="muted" class="hint">{t('discord.tickets.panelHelp')}</Text>

  <div class="setting-row">
    <label class="tr-text" for="dc-panel-title">
      <Text as="span" size="sm">{t('discord.tickets.panelTitleLabel')}</Text>
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
      <Text as="span" size="sm">{t('discord.tickets.panelBodyLabel')}</Text>
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
      <Text as="span" size="sm">{t('discord.tickets.panelButtonLabel')}</Text>
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
      <Text as="span" size="sm">{t('discord.tickets.panelColorLabel')}</Text>
      <Text as="span" size="xs" tone="muted">{t('discord.tickets.panelColorHelp')}</Text>
    </label>
    <span class="colors">
      {#each SWATCHES as swatch (swatch)}
        <button
          type="button"
          class="swatch {panelColor === swatch ? 'on' : ''}"
          style="background: {swatch}"
          aria-label={t('discord.tickets.swatchLabel', { hex: swatch })}
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
    caption={t('discord.tickets.panelPreviewCaption')}
    title={panel.title}
    body={panel.body}
    button={panel.button}
    color={panel.color}
    limits={[
      { label: t('discord.tickets.panelTitleLabel'), n: draft.config.ticketPanelTitle.length, max: TICKET_PANEL_TITLE_MAX },
      { label: t('discord.tickets.panelBodyLabel'), n: draft.config.ticketPanelBody.length, max: TICKET_PANEL_BODY_MAX },
      { label: t('discord.tickets.panelButtonLabel'), n: draft.config.ticketPanelButton.length, max: TICKET_PANEL_BUTTON_MAX }
    ]}
  />

  {#snippet after()}
    <div class="repost">
      <Text size="sm" tone="muted" class="hint">{t('discord.tickets.repostHelp')}</Text>
      <form method="POST" action="?/repost" use:enhance={repostSubmit}>
        <Button variant="secondary" type="submit" busy={draft.busy} disabled={!ticketsOn}>
          {t('discord.tickets.repostCta')}
        </Button>
      </form>
    </div>
  {/snippet}
</GuildForm>
