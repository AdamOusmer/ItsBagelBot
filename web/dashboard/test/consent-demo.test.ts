// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { TERMS_VERSION } from '@bagel/kit/consent';
import { actionEvent } from './analytics';
import { privateEnv, stubSvelteKit } from './sveltekit';

stubSvelteKit({ dev: true });
privateEnv.DEMO = '1';
privateEnv.BUSINESS_MAILING_ADDRESS = 'test mailing address';

const rpc = mock(async () => { throw new Error('unexpected RPC in demo'); });
const nats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({ ...nats, rpc }));

const { load, actions } = await import('../src/routes/consent/+page.server');

test('demo owners can load and submit consent without a signed-in session or RPCs', async () => {
  const url = new URL('https://dashboard.itsbagelbot.com/consent?next=%2Fsettings');
  const locals = { session: null, locale: 'en' };
  const { consent } = await load({ locals, url } as never) as { consent: { requestId: string } };
  const { request } = actionEvent({ consent: 'yes', terms_version: TERMS_VERSION, request_id: consent.requestId, marketing: 'yes' });
  const result = await Promise.resolve().then(() => actions.default({ locals, request, url } as never)).catch((err) => err);

  expect(consent).toMatchObject({ requestId: expect.stringMatching(/^[\da-f-]{36}$/), mailingAddress: privateEnv.BUSINESS_MAILING_ADDRESS });
  expect(result).toMatchObject({ status: 303, location: '/settings' });
  expect(rpc).not.toHaveBeenCalled();
});
