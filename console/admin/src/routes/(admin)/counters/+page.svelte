<script lang="ts">
  // Bot-global counters: the reserved loyalty namespace shared across every
  // channel (e.g. the personality module's lifetime tallies). Managers create,
  // set and delete them here; system modules bump them from Go. Broadcasters
  // never see this namespace.
  import { enhance } from '$app/forms';
  import { invalidateAll } from '$app/navigation';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Button, PageHead, AlertBanner, ConfirmDialog, Skeleton, toast } from '@bagel/shared';
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
  let busy = $state(false);
  // Per-row absolute value drafts, seeded from the loaded list.
  let drafts = $state<Record<string, number>>({});
  $effect(() => {
    for (const c of counters) if (!(c.name in drafts)) drafts[c.name] = c.value;
  });

  let deleteTarget = $state<string | null>(null);
  let deleteForm = $state<HTMLFormElement | null>(null);

  type ActionResult = { ok?: boolean; error?: string };
  const submit: SubmitFunction = () => {
    busy = true;
    return async ({ result }) => {
      busy = false;
      deleteTarget = null;
      const payload =
        result.type === 'success' || result.type === 'failure' ? (result.data as ActionResult | undefined) : undefined;
      if (result.type === 'success' && payload?.ok) {
        toast('ok', 'Saved.');
        newName = '';
        await invalidateAll();
        return;
      }
      toast('err', payload?.error ?? 'That did not work.');
    };
  };
</script>

<section class="screen active">
  <PageHead eyebrow="Access" description="Shared bot-wide tallies (reserved namespace). System modules bump these; channels never see them.">
    Bot <em>counters</em>
  </PageHead>

  {#if bundle?.degraded}
    <AlertBanner>Loyalty service unreachable — counters are temporarily unavailable.</AlertBanner>
  {/if}

  <form method="POST" action="?/create" class="create-row" use:enhance={submit}>
    <input class="search" name="name" placeholder="e.g. feeds" maxlength="64" bind:value={newName} required />
    <Button variant="primary" icon="plus" type="submit" loading={busy}>Create</Button>
  </form>

  {#if bundle === null}
    <Skeleton lines={3} />
  {:else if counters.length === 0}
    <p class="mut">No bot counters yet. Create one above; system modules can bump it by name.</p>
  {:else}
    <div class="tbl-wrap">
      <table class="tbl">
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
                <form method="POST" action="?/set" class="set-row" use:enhance={submit}>
                  <input type="hidden" name="name" value={c.name} />
                  <input class="search num" type="number" name="value" step="1" bind:value={drafts[c.name]} />
                  <Button variant="ghost" type="submit" icon="check" loading={busy}>Set</Button>
                </form>
              </td>
              <td class="r">
                <Button variant="destructive" icon="trash" onclick={() => (deleteTarget = c.name)}>Delete</Button>
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
  busy={busy}
  onCancel={() => (deleteTarget = null)}
  onConfirm={() => deleteForm?.requestSubmit()}
/>
<form method="POST" action="?/delete" use:enhance={submit} bind:this={deleteForm} hidden>
  <input type="hidden" name="name" value={deleteTarget ?? ''} />
</form>

<style>
  .create-row { display: flex; gap: 10px; align-items: center; margin: 0 0 18px; max-width: 420px; }
  .create-row input { flex: 1; min-width: 0; }

  .mut { font-family: var(--bb-font-body); font-size: 13px; color: var(--bb-muted); }

  .tbl-wrap { overflow-x: auto; -webkit-overflow-scrolling: touch; }
  .tbl { width: 100%; border-collapse: collapse; font-family: var(--bb-font-body); font-size: 13px; }
  .tbl th[scope='col'] {
    text-align: left;
    font-size: 11px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--bb-muted);
    padding: 4px 8px;
    border-bottom: 1px solid var(--bb-border);
    font-weight: 600;
  }
  .tbl td,
  .tbl th[scope='row'] { padding: 8px; border-bottom: 1px solid rgba(240, 236, 228, 0.05); color: var(--bb-white); }
  .tbl th[scope='row'] { text-align: left; font-weight: 600; }
  .tbl .r { text-align: right; }

  .set-row { display: inline-flex; gap: 8px; align-items: center; justify-content: flex-end; }
  .num { width: 120px; }
</style>
