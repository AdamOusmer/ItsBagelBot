<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { deserialize } from '$app/forms';
  import {
    Button,
    Chip,
    Code,
    FieldError,
    Icon,
    Input,
    Label,
    PickerOption,
    PickerPanel,
    Select,
    Tag,
    Text,
    getI18n,
    type CounterScope
  } from '@bagel/kit';

  const { t } = getI18n();

  type CounterRef = { name: string; scope: CounterScope };

  let { onInsert }: { onInsert: (token: string) => void } = $props();

  const uid = $props.id();
  let open = $state(false);
  let anchorEl = $state<HTMLDivElement>();
  let loaded = $state(false);
  let loading = $state(false);
  let counters = $state<CounterRef[]>([]);
  let newName = $state('');

  const COUNTS_FOR = ['channel', 'viewer', 'target', 'command', 'viewer_command'] as const;
  let countsFor = $state<(typeof COUNTS_FOR)[number]>('channel');

  const newScope = $derived<CounterScope>(countsFor === 'target' ? 'viewer' : countsFor);
  const countTarget = $derived(countsFor === 'target');
  let creating = $state(false);
  let err = $state('');

  const scopeTag: Record<CounterScope, string> = {
    channel: t('counters.tagChannel'),
    viewer: t('counters.tagViewer'),
    command: t('counters.tagCommand'),
    viewer_command: t('counters.tagViewerCommand')
  };
  const countsForLabel: Record<(typeof COUNTS_FOR)[number], string> = {
    channel: t('counters.scopeChannel'),
    viewer: t('counters.scopeViewer'),
    target: t('counters.pickerTarget'),
    command: t('counters.scopeCommand'),
    viewer_command: t('counters.scopeViewerCommand')
  };

  async function toggle() {
    open = !open;
    if (!open || loaded) return;
    loading = true;
    try {
      const res = await fetch('/counters/list');
      const data = (await res.json()) as { counters?: CounterRef[] };
      counters = data.counters ?? [];
      loaded = true;
    } catch {
    }
    loading = false;
  }

  function pick(name: string) {
    onInsert(countTarget ? `{counter:target:${name}}` : `{counter:${name}}`);
    open = false;
  }

  function norm(raw: string): string {
    return raw.trim().replace(/^!/, '').toLowerCase().slice(0, 64);
  }

  async function create() {
    const name = norm(newName);
    if (!name) {
      err = t('counters.errName');
      return;
    }
    err = '';
    creating = true;
    const body = new FormData();
    body.set('name', name);
    body.set('scope', newScope);
    try {
      const res = await fetch('/counters?/create', { method: 'POST', body });
      const r = deserialize(await res.text());
      const ok = r.type === 'success' && (r.data as { ok?: boolean } | undefined)?.ok === true;
      if (ok) {
        if (!counters.some((c) => c.name === name)) counters = [...counters, { name, scope: newScope }];
        newName = '';
        pick(name);
      } else {
        err = t('counters.toastFailed');
      }
    } catch {
      err = t('counters.toastFailed');
    }
    creating = false;
  }
</script>

<div class="cp" bind:this={anchorEl}>
  <Chip
    tone="muted"
    title={t('vars.counter.hint')}
    aria-haspopup="dialog"
    aria-expanded={open}
    onclick={toggle}
  >
    {t('commandEditor.pickCounter')}
    <Icon name="chevron" size={12} />
  </Chip>

  <PickerPanel {open} anchor={anchorEl} label={t('counters.pickerTitle')} width={280} maxHeight={360} onClose={() => (open = false)}>
    {#snippet children()}
      <div class="counts-for">
        <Label mono htmlFor="{uid}-scope">{t('counters.fieldScope')}</Label>
        <Select
          id="{uid}-scope"
          fill
          bind:value={countsFor}
          options={COUNTS_FOR.map((s) => ({ value: s, label: countsForLabel[s] }))}
        />
      </div>
      {#if countTarget}
        <div class="preview">
          <Text as="span" size="xs"><Code tone="positive">{'{counter:target:'}{newName || 'name'}{'}'}</Code></Text>
        </div>
      {/if}

      <Label mono as="span">{t('counters.pickerExisting')}</Label>
      {#if loading}
        <Text size="xs" tone="muted" role="status">{t('common.loading')}</Text>
      {:else if counters.length === 0}
        <Text size="xs" tone="muted">{t('counters.pickerEmpty')}</Text>
      {:else}
        <ul class="opts">
          {#each counters.toSorted((a, b) => a.name.localeCompare(b.name)) as c (c.name)}
            <PickerOption as="li" label={c.name} onclick={() => pick(c.name)}>
              {#snippet trail()}<Tag tone="bare">{scopeTag[c.scope]}</Tag>{/snippet}
            </PickerOption>
          {/each}
        </ul>
      {/if}

      <div class="new-head"><Label mono as="span">{t('counters.pickerNew')}</Label></div>
      <Input
        placeholder={t('counters.fieldNamePh')}
        maxlength="64"
        bind:value={newName}
        onkeydown={(e: KeyboardEvent) => e.key === 'Enter' && (e.preventDefault(), create())}
      />
      <FieldError message={err} />
      <div class="create">
        <Button variant="add" disabled={creating} onclick={create}>
          {creating ? t('counters.creating') : t('counters.pickerCreate')}
        </Button>
      </div>
    {/snippet}
  </PickerPanel>
</div>

<style>
  .cp { position: relative; display: inline-flex; }

  .new-head { margin-top: 4px; padding-top: 8px; border-top: 1px solid var(--bb-border); }

  .opts { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }

  .counts-for { display: flex; flex-direction: column; gap: 5px; }
  .preview {
    margin: -2px 0 2px;
    padding-left: 24px;
    opacity: 0.85;
  }

  .create { --btn-w: 100%; --btn-justify: center; }
</style>
