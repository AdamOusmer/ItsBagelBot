// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { FlowCard } from '#lib/flow/cards';
import { NEW_FLOW_ID, type FlowRef } from '#lib/flow/ref';
import type { DeleteReply, RewardReply, SaveReply, ToggleReply } from '#lib/flow/replies';
import type { ManagedReward } from '#lib/server/flow-reward';
import { newEditor, type EditorData, type Lowered } from '#lib/server/flow-page';
import { demoCards, demoDelete, demoEditor, demoRewardSave, demoSave, demoToggle } from '../flow-fixtures';

export * from '#lib/server/flow-page';

export async function liveCards(): Promise<FlowCard[]> {
  return demoCards();
}

export async function liveEditor(_locals: App.Locals, routeId: string): Promise<EditorData | null> {
  return routeId === NEW_FLOW_ID ? newEditor() : demoEditor(routeId);
}

export async function liveSave(_locals: App.Locals, lowered: Lowered): Promise<SaveReply> {
  return demoSave(lowered);
}

export async function liveRewardSave(locals: App.Locals, draft: ManagedReward): Promise<RewardReply> {
  return demoRewardSave(locals.locale, draft);
}

export async function liveToggle(_locals: App.Locals, ref: FlowRef, enabled: boolean, revision?: string): Promise<ToggleReply> {
  return demoToggle(ref, enabled, revision);
}

export async function liveDelete(_locals: App.Locals, ref: FlowRef): Promise<DeleteReply> {
  return demoDelete(ref);
}
