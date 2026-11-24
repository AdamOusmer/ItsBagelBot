// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { text } from '@sveltejs/kit';
import { env } from '@bagel/kit/server/env';
import { logger } from '@bagel/kit/server/logger';
import { parseResendEvent, planFor, type EventPlan } from '#lib/server/resend-events';
import { recordDelivery, suppressAddress } from '#lib/server/services';
import { verifySvix } from '#lib/server/svix';
import type { RequestHandler } from './$types';

const MAX_BODY_BYTES = 64 * 1024;

async function apply(plan: EventPlan): Promise<void> {
  for (const suppression of plan.suppressions) await suppressAddress(suppression);
  if (plan.delivery) await recordDelivery(plan.delivery);
}

export const POST: RequestHandler = async ({ request }) => {
  const secret = env.RESEND_WEBHOOK_SECRET?.trim();
  if (!secret) return text('Webhook not configured', { status: 503 });

  const body = await request.text();
  if (Buffer.byteLength(body) > MAX_BODY_BYTES) return text('Payload too large', { status: 413 });
  if (!verifySvix({ secret, headers: request.headers, body })) return text('Invalid signature', { status: 401 });

  const event = parseResendEvent(body);
  if (!event) return text('Invalid payload', { status: 400 });

  try {
    await apply(planFor(event));
  } catch (err) {
    logger.error({ err, type: event.type }, '[resend-webhook] apply failed');
    return text('Unavailable', { status: 500 });
  }
  return text('ok', { status: 200 });
};
