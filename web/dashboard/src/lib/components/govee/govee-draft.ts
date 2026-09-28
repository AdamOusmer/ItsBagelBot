// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { moduleDef, namespaceReplyTemplate, type GoveeBinding, type GoveeDevice } from '@bagel/kit';

export interface GoveeDraft {
  title: string;
  cost: number;
  color: string;
  cooldown: number;
  onRedeem: string;
  replyMessage: string;
  allowOff: boolean;
  liveOnly: boolean;
}

const COST_MAX = 10_000_000;
export const GOVEE_COOLDOWN_MAX = 604_800;

const reply = moduleDef('govee')!.replies.find((r) => r.key === 'reply')!;

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

export type GoveeErrorField = 'title' | 'cost' | 'cooldown';
export type GoveeErrors = Partial<Record<GoveeErrorField, string>>;

const whole = (n: unknown, min: number, max: number) =>
  Number.isInteger(n) && (n as number) >= min && (n as number) <= max;

export function goveeErrors(draft: GoveeDraft): GoveeErrors {
  const errors: GoveeErrors = {};
  if (!draft.title.trim()) errors.title = 'govee.errTitleRequired';
  if (!whole(draft.cost, 1, COST_MAX)) errors.cost = 'govee.errCost';
  if (!whole(draft.cooldown, 0, GOVEE_COOLDOWN_MAX)) errors.cooldown = 'govee.errCooldown';
  return errors;
}

export function goveeFormFields(draft: GoveeDraft): Record<string, string> {
  return {
    title: draft.title,
    cost: String(draft.cost),
    color: draft.color,
    cooldown: String(draft.cooldown),
    onRedeem: draft.onRedeem,
    replyMessage: namespaceGoveeReply(draft.replyMessage),
    allow_off: draft.allowOff ? 'on' : '',
    allow_offline: draft.liveOnly ? '' : 'on'
  };
}
