<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Input from '@bagel/ui/svelte/Input.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import Textarea from '@bagel/ui/svelte/Textarea.svelte';
  import {
    getI18n,
    tDiscordModuleField,
    tDiscordModuleOption,
    type DiscordModuleField,
    type DiscordConfig
  } from '@bagel/kit';
  import type { Snippet } from 'svelte';
  import type { DiscordLayout } from '$lib/server/discord-store';
  import type { ModuleDraft } from '$lib/discord/module-draft.svelte';
  import { FIELD_LIMITS, PLACEHOLDER_DEFAULTS, SEGMENTED_NUMBERS } from '$lib/discord/module-view';
  import { categoriesOf, rolesOf, textChannelsOf, voiceChannelsOf } from '$lib/discord/guild-view';
  import ChannelListField from './ChannelListField.svelte';
  import ChannelPicker from './ChannelPicker.svelte';
  import ColorField from './ColorField.svelte';
  import FieldNote from './FieldNote.svelte';
  import NameListField from './NameListField.svelte';
  import PinnedRolesField from './PinnedRolesField.svelte';
  import RoleChecklist from './RoleChecklist.svelte';
  import RoleSelect from './RoleSelect.svelte';
  import SwitchRow from './SwitchRow.svelte';

  let {
    draft,
    layout,
    afterField
  }: { draft: ModuleDraft; layout: DiscordLayout | undefined; afterField?: Snippet<[keyof DiscordConfig]> } = $props();

  const { t } = getI18n();

  const text = $derived(textChannelsOf(layout));
  const voice = $derived(voiceChannelsOf(layout));
  const categories = $derived(categoriesOf(layout));
  const roles = $derived(rolesOf(layout));
  const moduleId = $derived(draft.def.id);

  const copy = (f: DiscordModuleField, part: 'label' | 'help' | 'placeholder') =>
    tDiscordModuleField(t, moduleId, f, part);
  const hintOf = (f: DiscordModuleField) => copy(f, 'placeholder') || PLACEHOLDER_DEFAULTS[f.key] || '';
  const valueOf = (f: DiscordModuleField) => String(draft.config[f.key] ?? '');

  function selectValue(f: DiscordModuleField): string {
    return valueOf(f) || f.options?.[0]?.value || '';
  }

  const missingVoiceCategory = $derived(draft.config.voiceHubId.trim() !== '' && draft.config.voiceCategoryId.trim() === '');
</script>

{#each draft.def.fields as f (f.key)}
  {#if f.type === 'toggle'}
    <SwitchRow
      {draft}
      invalid={draft.invalid}
      field={f.key}
      label={copy(f, 'label')}
      help={copy(f, 'help')}
      defaultOn={f.defaultOn !== false}
    />
  {:else if f.type === 'channel' || f.type === 'category'}
    <ChannelPicker
      {draft}
      invalid={draft.invalid}
      field={f.key}
      label={copy(f, 'label')}
      help={copy(f, 'help')}
      options={f.type === 'category' ? categories : f.key === 'voiceHubId' ? voice : text}
      prefix={f.type === 'category' || f.key === 'voiceHubId' ? '' : '#'}
      required={f.key === 'voiceCategoryId' ? t('discord.module.voiceCategoryRequired') : ''}
      missing={missingVoiceCategory}
    />
  {:else if f.type === 'role'}
    <RoleSelect {draft} field={f.key} label={copy(f, 'label')} help={copy(f, 'help')} {roles} />
  {:else if f.type === 'roleList'}
    <RoleChecklist {draft} field={f.key} label={copy(f, 'label')} help={copy(f, 'help')} {roles} />
  {:else if f.type === 'channelList'}
    <ChannelListField {draft} field={f.key} label={copy(f, 'label')} help={copy(f, 'help')} channels={[...text, ...voice]} />
  {:else if f.type === 'nameList'}
    <NameListField {draft} field={f.key} label={copy(f, 'label')} help={copy(f, 'help')} placeholder={copy(f, 'placeholder')} />
  {:else if f.type === 'pinnedRoles'}
    <PinnedRolesField {draft} label={copy(f, 'label')} help={copy(f, 'help')} />
  {:else if f.type === 'color'}
    <ColorField {draft} field={f.key} label={copy(f, 'label')} help={copy(f, 'help')} />
  {:else if f.type === 'select'}
    <div class="setting-row">
      <label class="tr-text" for="dc-{f.key}">
        <Text as="span" size="sm">{copy(f, 'label')}</Text>
        <Text as="span" size="xs" tone="muted" id="dch-{f.key}">{copy(f, 'help')}</Text>
      </label>
      <span class="setting-picker">
        <Select
          fill
          id="dc-{f.key}"
          aria-describedby="dch-{f.key}"
          value={selectValue(f)}
          onchange={(e) => draft.set(f.key, e.currentTarget.value)}
          options={(f.options ?? []).map((opt) => ({ value: opt.value, label: tDiscordModuleOption(t, moduleId, f.key, opt) }))}
        />
      </span>
      <FieldNote invalid={draft.invalid} field={f.key} />
    </div>
  {:else if f.type === 'textarea'}
    <div class="setting-row stacked">
      <label class="tr-text" for="dc-{f.key}">
        <Text as="span" size="sm">{copy(f, 'label')}</Text>
        <Text as="span" size="xs" tone="muted" id="dch-{f.key}">{copy(f, 'help')}</Text>
      </label>
      <Textarea
        id="dc-{f.key}"
        aria-describedby="dch-{f.key}"
        fill
        rows={4}
        maxlength={FIELD_LIMITS[f.key]?.max}
        placeholder={hintOf(f)}
        value={valueOf(f)}
        oninput={(e: Event & { currentTarget: HTMLTextAreaElement }) => draft.set(f.key, e.currentTarget.value)}
      />
      <FieldNote invalid={draft.invalid} field={f.key} />
    </div>
  {:else if f.type === 'number' && SEGMENTED_NUMBERS[f.key]}
    <div class="setting-row">
      <span class="tr-text">
        <Text as="span" size="sm">{copy(f, 'label')}</Text>
        <Text as="span" size="xs" tone="muted">{copy(f, 'help')}</Text>
      </span>
      <SegmentedControl
        options={SEGMENTED_NUMBERS[f.key] ?? []}
        label={copy(f, 'label')}
        bind:value={() => valueOf(f) || (SEGMENTED_NUMBERS[f.key]?.[0] ?? ''), (v) => draft.set(f.key, v)}
      />
      <FieldNote invalid={draft.invalid} field={f.key} />
    </div>
  {:else}
    <div class="setting-row">
      <label class="tr-text" for="dc-{f.key}">
        <Text as="span" size="sm">{copy(f, 'label')}</Text>
        <Text as="span" size="xs" tone="muted" id="dch-{f.key}">{copy(f, 'help')}</Text>
      </label>
      <Input
        id="dc-{f.key}"
        aria-describedby="dch-{f.key}"
        type={f.type === 'number' ? 'number' : 'text'}
        inputmode={f.type === 'number' ? 'numeric' : undefined}
        min={FIELD_LIMITS[f.key]?.min}
        max={f.type === 'number' ? FIELD_LIMITS[f.key]?.max : undefined}
        maxlength={f.type === 'number' ? undefined : FIELD_LIMITS[f.key]?.max}
        placeholder={hintOf(f)}
        value={valueOf(f)}
        oninput={(e: Event & { currentTarget: HTMLInputElement }) => draft.set(f.key, e.currentTarget.value)}
      />
      <FieldNote invalid={draft.invalid} field={f.key} />
    </div>
  {/if}
  {#if afterField}{@render afterField(f.key)}{/if}
{/each}
