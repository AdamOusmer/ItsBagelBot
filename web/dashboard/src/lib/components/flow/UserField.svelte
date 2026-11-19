<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { deserialize } from '$app/forms';
  import { actionPayload, getI18n } from '@bagel/kit';
  import { viewerIdKey, viewerList } from '@bagel/kit/automations';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { FlowNode } from '#lib/flow/model';
  import { configText } from '#lib/flow/setup';

  let { node, id, label, placeholder, tag, error, fixedStatus = false, multiple = false }: {
    node: FlowNode; id: string; label: string; placeholder?: string; tag?: string; error?: string; fixedStatus?: boolean; multiple?: boolean;
  } = $props();

  type Resolved = { ok: boolean; userId?: string; login?: string; code?: string };

  const { t } = getI18n();

  let draft = $state('');
  const login = $derived(multiple ? draft : configText(node, 'login'));
  const userId = $derived(multiple ? '' : String(node.config.userId ?? ''));
  const viewers = $derived(multiple ? viewerList(node.config) : []);
  const matched = $derived(userId !== '');
  let problem = $state<'' | 'missing' | 'failed'>('');
  let checking = $state(false);
  let pending = '';

  function onInput(value: string) {
    const entered = value.trim().replace(/^@/, '').toLowerCase();
    if (multiple) draft = entered;
    else {
      node.config.login = entered;
      delete node.config.userId;
    }
    problem = '';
  }

  function saveViewers(list: typeof viewers) {
    node.config.users = list.map(({ id, login }) => `${id}:${login}`).join(',');
    delete node.config.userId;
    delete node.config.login;
  }

  function addViewer(id: NonNullable<Resolved['userId']>, login: NonNullable<Resolved['login']>) {
    const picked = { id, login };
    const refreshed = viewers.filter((viewer) => viewer.id || viewer.login.toLowerCase() !== draft);
    const key = viewerIdKey(id);
    const existing = refreshed.findIndex((viewer) => viewerIdKey(viewer.id) === key);
    if (existing < 0) refreshed.push(picked);
    else refreshed[existing] = picked;
    saveViewers(refreshed);
    draft = '';
  }

  async function lookup(asked: string): Promise<Resolved | undefined> {
    const body = new FormData();
    body.set('login', asked);
    try {
      const res = await fetch('?/resolveUser', { method: 'POST', body, headers: { 'x-sveltekit-action': 'true' } });
      return actionPayload<Resolved>(deserialize(await res.text()));
    } catch {
      return undefined;
    }
  }

  function resolved(userId: NonNullable<Resolved['userId']>, name: NonNullable<Resolved['login']>) {
    if (multiple) addViewer(userId, name);
    else {
      node.config.userId = userId;
      node.config.login = name;
    }
    problem = '';
  }

  function finishLookup(asked: string, reply: Resolved | undefined) {
    if (multiple && pending !== asked) return;
    pending = '';
    checking = false;
    if (login !== asked) return;
    if (reply?.ok && reply.userId) resolved(reply.userId, reply.login ?? asked);
    else problem = reply?.code === 'missing' ? 'missing' : 'failed';
  }

  async function resolve() {
    const asked = login;
    if (!asked || matched) return;
    if (multiple && pending === asked) return;
    pending = asked;
    checking = true;
    finishLookup(asked, await lookup(asked));
  }

  function onKey(event: KeyboardEvent) {
    if (event.key !== 'Enter') return;
    if (multiple) event.preventDefault();
    void resolve();
  }
</script>

<Field {label} {tag} error={fixedStatus ? undefined : error} errorId="{id}-error" for={id}>
  <Input
    {id}
    {placeholder}
    aria-invalid={Boolean(error)}
    aria-describedby={error ? `${id}-error` : undefined}
    autocomplete="off"
    spellcheck="false"
    value={login || userId}
    oninput={(e) => onInput(e.currentTarget.value)}
    onblur={resolve}
    onkeydown={onKey}
  />
</Field>
<div class="status" class:fixed={fixedStatus} aria-live="polite">
  {#if fixedStatus && error}
    <Text size="sm" tone="danger" id="{id}-error"><bb-fit mode="block">{error}</bb-fit></Text>
  {:else if problem === 'missing'}
    <Text size="sm" tone="danger">{t('flows.userMissing')}</Text>
  {:else if problem === 'failed'}
    <Text size="sm" tone="danger">{t('flows.userFailed')}</Text>
  {:else if checking}
    <Text size="sm" tone="muted">{t('flows.userChecking')}</Text>
  {:else if matched && !login}
    <Text size="sm" tone="muted">{t('flows.userIdOnly')}</Text>
  {:else if matched}
    <Text size="sm" tone="muted">{t('flows.userFound')}</Text>
  {/if}
</div>
{#if multiple && viewers.length}
  <div class="viewers">
    {#each viewers as viewer, index}
      {@const name = viewer.login ? `@${viewer.login}` : viewer.id}
      <Chip tone={viewer.id ? 'muted' : 'danger'} onclick={() => saveViewers(viewers.filter((_, i) => i !== index))} aria-label={t('flows.step.condition.user.removeViewer', { name })}>
        <span>{name}</span>
        <Icon name="x" size={11} />
      </Chip>
      {#if !viewer.id}
        <Text size="sm" tone="muted">{t('flows.step.condition.user.repick')}</Text>
      {/if}
    {/each}
  </div>
{/if}

<style>
  .viewers { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; margin-top: 8px; }
  .status {
    min-height: 20px;
    margin-top: -8px;
  }
  .fixed { height: 28px; }
</style>
