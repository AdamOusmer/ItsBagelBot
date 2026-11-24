// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc } from '@bagel/kit/server/nats';

const TIMEOUT = 5000;

export type CampaignStatus = 'draft' | 'approved' | 'sending' | 'paused' | 'done';

export type CampaignContent = { subject: string; html: string; text: string };

export type CampaignWire = {
  id: number;
  name: string;
  revision: number;
  status: CampaignStatus;
  content: Record<string, CampaignContent> | null;
  created_by: string;
  created_at: string;
  approved_by?: string;
  approved_at?: string;
  sends: Record<string, number> | null;
};

export type CampaignAudience = { eligible: Record<string, number>; total: number };

type CampaignVerb =
  | 'campaign_list'
  | 'campaign_get'
  | 'campaign_save'
  | 'campaign_preview'
  | 'campaign_test'
  | 'campaign_approve'
  | 'campaign_pause'
  | 'campaign_resume';

type CampaignRequest = {
  actor_id: string;
  id?: number;
  name?: string;
  content?: Record<string, CampaignContent>;
  revision?: number;
};

type CampaignReply = {
  campaign?: CampaignWire;
  campaigns?: CampaignWire[];
  eligible?: Record<string, number>;
  eligible_total?: number;
};

export type CampaignRef = { actorId: string; id: number; revision?: number };

export type CampaignDraft = { actorId: string; id?: number; name: string; content: Record<string, CampaignContent> };

function call(verb: CampaignVerb, request: CampaignRequest): Promise<CampaignReply> {
  return rpc<CampaignReply>(`rpc.admin.marketing.${verb}`, request, TIMEOUT);
}

function requireCampaign(reply: CampaignReply): CampaignWire {
  if (!reply.campaign) throw new Error('Campaign service returned no campaign.');
  return reply.campaign;
}

export async function campaignList(actorId: string): Promise<CampaignWire[]> {
  const reply = await call('campaign_list', { actor_id: actorId });
  return reply.campaigns ?? [];
}

export async function campaignGet({ actorId, id }: CampaignRef): Promise<CampaignWire> {
  return requireCampaign(await call('campaign_get', { actor_id: actorId, id }));
}

export async function campaignAudience({ actorId, id }: CampaignRef): Promise<CampaignAudience> {
  const reply = await call('campaign_preview', { actor_id: actorId, id });
  return { eligible: reply.eligible ?? {}, total: reply.eligible_total ?? 0 };
}

export async function campaignSave(draft: CampaignDraft): Promise<CampaignWire> {
  const { actorId, ...fields } = draft;
  return requireCampaign(await call('campaign_save', { actor_id: actorId, ...fields }));
}

export async function campaignTest({ actorId, id }: CampaignRef): Promise<void> {
  await call('campaign_test', { actor_id: actorId, id });
}

async function transition(verb: CampaignVerb, { actorId, id, revision }: CampaignRef): Promise<CampaignWire> {
  return requireCampaign(await call(verb, { actor_id: actorId, id, revision }));
}

export const campaignApprove = (ref: CampaignRef) => transition('campaign_approve', ref);

export const campaignPause = (ref: CampaignRef) => transition('campaign_pause', ref);

export const campaignResume = (ref: CampaignRef) => transition('campaign_resume', ref);
