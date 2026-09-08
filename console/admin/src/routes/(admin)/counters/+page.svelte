<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Bot-global counters: the reserved loyalty namespace shared across every
  // channel (e.g. the personality module's lifetime tallies). Managers create,
  // set and delete them here; system modules bump them from Go. Broadcasters
  // never see this namespace.
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Button,
    PageHead,
    AlertBanner,
    ConfirmDialog,
    Skeleton,
    toast,
    actionPayload,
    adminToastFailure
  } from '@bagel/shared';

  const failed = adminToastFailure(toast);
  import type { BotCountersBundle } from './+page.server';

  let { data } = $props();

  // Streamed bundle -> local state; refreshed via invalidateAll after writes.
  let bundle = $state<BotCountersBundle | null>(null);
  $effect(() => {
    let alive = true;
    data.bundle.then((b: BotCountersBundle) => {
      if (alive) bundle = b;
    });
    return () => {
      alive = false;
    };
  });

  const counters = $derived(bundle?.counters ?? []);

  let newName = $state('');
  // One in-flight key ('create', 'set:<name>' or 'delete') so a row's Set
  // spinner never lights up every other button, mirroring the dashboard's
  // per-row save states.
  let busyKey = $state<string | null>(null);
  // Per-row absolute value drafts, seeded from the loaded list.
  let drafts = $state<Record<string, number>>({});
  $effect(() => {
    for (const c of counters) if (!(c.name in drafts)) drafts[c.name] = c.value;
  });

  let deleteTarget = $state<string | null>(null);
  let deleteForm = $state<HTMLFormElement | null>(null);

  function submitAs(key: string, okNotice: string): SubmitFunction {
    return () => {
      busyKey = key;
      return async ({ result }) => {
        busyKey = null;
        deleteTarget = null;
        const payload = actionPayload(result);
        if (result.type === 'success' && payload?.ok) {
          toast('ok', okNotice);
          newName = '';
          await invalidateAll();
          return;
        }
        failed(payload, "That didn't work. Try again.");
      };
    };
  }
</script>

<section class="screen active">
  <PageHead eyebrow="Access" description="Shared bot-wide tallies (reserved namespace). System modules bump these; channels never see them.">
    Bot <em>counters</em>
  </PageHead>

  {#if bundle?.degraded}
    <AlertBanner>Loyalty service unreachable. Counters are temporarily unavailable.</AlertBanner>
  {/if}

  <form method="POST" action="?/create" class="create-row" use:enhance={submitAs('create', 'Counter created.')}>
    <input class="search" name="name" placeholder="e.g. feeds" maxlength="64" bind:value={newName} required />
    <Button variant="primary" type="submit" loading={busyKey === 'create'}>Create</Button>
  </form>

  {#if bundle === null}
    <Skeleton lines={3} />
  {:else if counters.length === 0}
    <p class="mut">No bot counters yet. Create one above; system modules can bump it by name.</p>
  {:else}
    <div class="bb-tbl-wrap">
      <table class="bb-tbl">
        <thead>
          <tr>
            <th scope="col">Counter</th>
            <th scope="col" class="r">Value</th>
            <th scope="col" class="r">Set</th>
            <th scope="col" class="r">Remove</th>
          </tr>
        </thead>
        <tbody>
          {#each counters.toSorted((a, b) => a.name.localeCompare(b.name)) as c (c.name)}
            <tr>
              <th scope="row"><code>{c.name}</code></th>
              <td class="r">{c.value.toLocaleString()}</td>
              <td class="r">
                <form method="POST" action="?/set" class="set-row" use:enhance={submitAs(`set:${c.name}`, 'Counter updated.')}>
                  <input type="hidden" name="name" value={c.name} />
                  <input class="search num" type="number" name="value" step="1" bind:value={drafts[c.name]} />
                  <Button variant="ghost" type="submit" loading={busyKey === `set:${c.name}`}>Set</Button>
                </form>
              </td>
              <td class="r">
                <Button variant="destructive" onclick={() => (deleteTarget = c.name)}>Delete</Button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</section>

<ConfirmDialog
  open={deleteTarget !== null}
  title="Delete this bot counter?"
  body={`"${deleteTarget ?? ''}" and its value are removed for the whole bot. Modules that bump it will recreate it from zero.`}
  confirmLabel="Delete"
  cancelLabel="Cancel"
  danger
  busy={busyKey === 'delete'}
  onCancel={() => (deleteTarget = null)}
  onConfirm={() => deleteForm?.requestSubmit()}
/>
<form method="POST" action="?/delete" use:enhance={submitAs('delete', 'Counter deleted.')} bind:this={deleteForm} hidden>
  <input type="hidden" name="name" value={deleteTarget ?? ''} />
</form>

<style>
  .create-row { display: flex; gap: 10px; align-items: center; margin: 0 0 18px; max-width: 420px; }
  .create-row input { flex: 1; min-width: 0; }

  .mut { font-family: var(--bb-font-body); font-size: 13px; color: var(--bb-muted); }

  /* Roomier rows than the shared default: this table is short and the extra
     pixel of leading reads better against the create form above it. */
  .bb-tbl td,
  .bb-tbl th[scope='row'] { padding: 8px; }

  .set-row { display: inline-flex; gap: 8px; align-items: center; justify-content: flex-end; }
  .num { width: 120px; }
</style>
