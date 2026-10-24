// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { namespaceReplyTemplate, type GoveeBinding, type GoveeDevice } from '@bagel/kit';
import { GOVEE_MODULE } from '@bagel/kit/catalog/govee';
import {
  rewardCoreErrors,
  rewardCoreFormFields,
  type RewardCoreDraft,
  type RewardCoreErrorField,
  type RewardCoreErrors
} from '$lib/reward-fields';

export interface GoveeDraft extends RewardCoreDraft {
  allowOff: boolean;
  liveOnly: boolean;
}

const reply = GOVEE_MODULE.replies.find((r) => r.key === 'reply')!;

export function namespaceGoveeReply(message: string): string {
  return namespaceReplyTemplate('govee', reply, message);
}

type RewardFields = Pick<GoveeDraft, 'title' | 'cost' | 'color' | 'cooldown'>;
type BindingFields = Omit<GoveeDraft, keyof RewardFields>;

function rewardFields(reward: GoveeBinding['reward'] | null, fallbackTitle: () => string): RewardFields {
  return {
    title: reward?.title || fallbackTitle(),
    cost: reward?.cost ?? 500,
    color: reward?.color || '#9147ff',
    cooldown: reward?.cooldown ?? 0
  };
}

function bindingFields(binding: GoveeBinding | null): BindingFields {
  return {
    onRedeem: binding?.onRedeem ?? 'fulfill',
    replyMessage: namespaceGoveeReply(binding?.replyMessage ?? ''),
    allowOff: binding?.allowOff ?? false,
    liveOnly: !binding?.allowOffline
  };
}

export function goveeDraftFor(
  device: GoveeDevice,
  binding: GoveeBinding | null,
  defaultTitle: (name: string) => string
): GoveeDraft {
  return {
    ...rewardFields(binding?.reward ?? null, () => defaultTitle(device.name)),
    ...bindingFields(binding)
  };
}

export type GoveeErrorField = RewardCoreErrorField;
export type GoveeErrors = RewardCoreErrors;

export const GOVEE_ERROR_KEYS: Readonly<Record<GoveeErrorField, string>> = Object.freeze({
  title: 'govee.errTitleRequired',
  cost: 'govee.errCost',
  cooldown: 'govee.errCooldown'
});

export const goveeErrors = (draft: GoveeDraft): GoveeErrors => rewardCoreErrors(draft, GOVEE_ERROR_KEYS);

export function goveeFormFields(draft: GoveeDraft): Record<string, string> {
  return {
    ...rewardCoreFormFields(draft, namespaceGoveeReply),
    allow_off: draft.allowOff ? 'on' : '',
    allow_offline: draft.liveOnly ? '' : 'on'
  };
}
