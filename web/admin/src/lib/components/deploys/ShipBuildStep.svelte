<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit/i18n/context';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Label from '@bagel/ui/svelte/Label.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { DeployPlan } from '#lib/deploys/types';
  import CommitList from './CommitList.svelte';
  import type { ShipState } from './ship';
  import { shortSha } from './view';

  let {
    plan,
    ship,
    version = $bindable()
  }: {
    plan: DeployPlan | null;
    ship: ShipState;
    version: string;
  } = $props();

  const { t } = getI18n();
</script>

<Stack gap={4}>
  <div class="build-fields">
    <Grid cols={2} gap={3}>
      {#if ship.uses.version}
        <Field label={t('admin.deploys.version')} for="ship-version" fit>
          <Input id="ship-version" mono fill bind:value={version} required />
        </Field>
      {/if}
      <Stack gap={1}>
        <Label as="span" id="ship-target-label"><bb-fit><span data-fit>{t('admin.deploys.target')}</span></bb-fit></Label>
        <output class="target" aria-labelledby="ship-target-label">
          <Text as="span" mono tone="pale">{shortSha(ship.targetSha) || '-'}</Text>
        </output>
      </Stack>
    </Grid>
  </div>
  <Card flush>
    <div class="commit-list"><CommitList commits={plan?.commits ?? []} since={plan?.last_tag ?? '-'} open /></div>
  </Card>
</Stack>

<style>
  .build-fields {
    max-inline-size: 35rem;
  }

  .target {
    display: flex;
    align-items: center;
    min-block-size: 38px;
  }

  .commit-list {
    padding: var(--bb-space-2) var(--bb-space-4);
  }
</style>
