<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AdminUserWire } from '#lib/server/services';
  import { actionLabel, type UserActionDef } from './user-actions';

  let { actions, user, busy, onAction, children }: {
    actions: UserActionDef[];
    user: AdminUserWire;
    busy: string | null;
    onAction: (def: UserActionDef) => void;
    children?: Snippet;
  } = $props();

  const { t } = getI18n();
</script>

<div class="actions">
  {#each actions as action (action.id === 'unban' ? 'ban' : action.id)}
    <Button
      variant={action.danger ? 'primary' : 'ghost'}
      tone={action.danger ? 'danger' : 'neutral'}
      disabled={busy !== null}
      busy={busy === action.id}
      onclick={() => onAction(action)}
      block
    >
      <bb-fit><span data-fit>{t(actionLabel(action, user))}</span></bb-fit>
    </Button>
  {/each}
  {@render children?.()}
</div>

<style>
  .actions {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--bb-space-2);
  }
</style>
