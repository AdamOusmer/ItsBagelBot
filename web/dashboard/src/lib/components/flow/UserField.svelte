<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { deserialize } from '$app/forms';
  import { actionPayload, getI18n } from '@bagel/kit';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { FlowNode } from '#lib/flow/model';

  let { node, id, label, placeholder }: { node: FlowNode; id: string; label: string; placeholder?: string } = $props();

  type Resolved = { ok: boolean; userId?: string; login?: string; code?: string };

  const { t } = getI18n();

  const login = $derived(String(node.config.login ?? ''));
  const userId = $derived(String(node.config.userId ?? ''));
  const matched = $derived(userId !== '');
  let problem = $state<'' | 'missing' | 'failed'>('');
  let checking = $state(false);

  function onInput(value: string) {
    node.config.login = value.trim().replace(/^@/, '').toLowerCase();
    delete node.config.userId;
    problem = '';
  }

  async function resolve() {
    const asked = login;
    if (!asked || matched) return;
    checking = true;
    const body = new FormData();
    body.set('login', asked);
    let reply: Resolved | undefined;
    try {
      const res = await fetch('?/resolveUser', { method: 'POST', body, headers: { 'x-sveltekit-action': 'true' } });
      reply = actionPayload<Resolved>(deserialize(await res.text()));
    } catch {
      reply = undefined;
    }
    checking = false;
    if (login !== asked) return;
    if (reply?.ok && reply.userId) {
      node.config.userId = reply.userId;
      problem = '';
    } else {
      problem = reply?.code === 'missing' ? 'missing' : 'failed';
    }
  }
</script>

<Field {label} for={id}>
  <Input
    {id}
    {placeholder}
    autocomplete="off"
    spellcheck="false"
    value={login || userId}
    oninput={(e) => onInput(e.currentTarget.value)}
    onblur={resolve}
    onkeydown={(e) => {
      if (e.key === 'Enter') void resolve();
    }}
  />
</Field>
<div class="status" aria-live="polite">
  {#if problem === 'missing'}
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

<style>
  .status {
    min-height: 20px;
    margin-top: -8px;
  }
</style>
