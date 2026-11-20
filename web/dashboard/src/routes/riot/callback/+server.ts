// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { rpcReply } from '@bagel/kit/server/nats';
import { logger } from '@bagel/kit/server/logger';
import { SUB, auditDashboardImpersonation } from '#lib/server/services';
import { setModuleEnabled } from '#lib/server/module-blob';
import { riotLinkStore, type RiotAccount } from '#lib/server/riot-store';
import { requireValorantActor, RIOT_MODULE_PATH, riotCallbackCode, riotConfigured, riotFail, riotRedirectURI } from '#lib/server/riot-oauth';

type CodeExchange = { channel_id: string; code: string; redirect_uri: string };
type CodeExchangeReply = Partial<RiotAccount> & { error?: string };

async function exchangeCode(request: CodeExchange): Promise<RiotAccount> {
  let reply: CodeExchangeReply;
  try {
    reply = await rpcReply<CodeExchangeReply>(`${SUB.gossip}.valorant.link`, request, 10000);
  } catch (err) {
    logger.warn({ err }, '[riot-callback] code exchange failed');
    riotFail('oauth');
  }
  const { error, puuid, game_name = '', tag_line = '', shard } = reply;
  if (error) {
    logger.warn({ err: error }, '[riot-callback] code exchange refused');
    riotFail('oauth');
  }
  if (!puuid || !shard) riotFail('oauth');
  return { puuid, game_name, tag_line, shard };
}

async function saveLink({ uid }: ReturnType<typeof requireValorantActor>, account: RiotAccount, locals: App.Locals): Promise<void> {
  const store = riotLinkStore(uid);
  try {
    const { linked } = await store.status();
    await store.set(account);
    auditDashboardImpersonation(locals.session, 'riot:link', '');
    if (!linked) await setModuleEnabled(uid, 'valorant', true);
  } catch (err) {
    logger.error({ err }, '[riot-callback] link setup failed');
    riotFail('store');
  }
}

export const GET: RequestHandler = async ({ url, cookies, locals }) => {
  const actor = requireValorantActor(locals);
  const code = riotCallbackCode(actor, cookies, url.searchParams);
  if (!riotConfigured()) riotFail('unconfigured');
  const account = await exchangeCode({ channel_id: actor.uid, code, redirect_uri: riotRedirectURI() });
  await saveLink(actor, account, locals);
  throw redirect(302, `${RIOT_MODULE_PATH}?linked=1`);
};
