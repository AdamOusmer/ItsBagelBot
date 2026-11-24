// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { createHmac, timingSafeEqual } from 'node:crypto';

const TOLERANCE_SEC = 300;
const SECRET_PREFIX = 'whsec_';

type Delivery = { secret: string; headers: Headers; body: string; nowMs?: number };

function signingKey(secret: string): Buffer {
  return Buffer.from(secret.startsWith(SECRET_PREFIX) ? secret.slice(SECRET_PREFIX.length) : secret, 'base64');
}

function fresh(timestamp: string, nowMs: number): boolean {
  const sentSec = Number(timestamp);
  return Number.isFinite(sentSec) && Math.abs(nowMs / 1000 - sentSec) <= TOLERANCE_SEC;
}

function matches(candidate: string, expected: Buffer): boolean {
  const [version, value] = candidate.split(',');
  if (version !== 'v1' || !value) return false;
  const got = Buffer.from(value, 'base64');
  return got.length === expected.length && timingSafeEqual(got, expected);
}

export function verifySvix({ secret, headers, body, nowMs = Date.now() }: Delivery): boolean {
  const parts = ['svix-id', 'svix-timestamp', 'svix-signature'].map((name) => headers.get(name));
  if (parts.some((part) => !part)) return false;
  const [id, timestamp, signatures] = parts as string[];
  if (!fresh(timestamp, nowMs)) return false;
  const expected = createHmac('sha256', signingKey(secret)).update(`${id}.${timestamp}.${body}`).digest();
  return signatures.split(' ').some((candidate) => matches(candidate, expected));
}
