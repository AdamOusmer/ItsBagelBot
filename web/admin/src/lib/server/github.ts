// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { createSign } from 'node:crypto';
import { env } from '@bagel/kit/server/env';

const API = 'https://api.github.com';
const TOKEN_MARGIN_MS = 60_000;

export const REPO = `${env.RELEASE_GITHUB_OWNER || 'AdamOusmer'}/${env.RELEASE_GITHUB_REPO || 'ItsBagelBot'}`;

export class GitHubError extends Error {
  constructor(
    readonly status: number,
    message: string
  ) {
    super(message);
  }
}

type Token = { value: string; expires: number };

let token: Token | null = null;

function base64url(data: string | Buffer): string {
  return Buffer.from(data).toString('base64url');
}

function appJwt(appId: string, key: string): string {
  const now = Math.floor(Date.now() / 1000);
  const head = base64url(JSON.stringify({ alg: 'RS256', typ: 'JWT' }));
  const body = base64url(JSON.stringify({ iat: now - 60, exp: now + 540, iss: appId }));
  const signature = createSign('RSA-SHA256').update(`${head}.${body}`).sign(key);
  return `${head}.${body}.${base64url(signature)}`;
}

function appCredentials(): { appId: string; installation: string; key: string } {
  const appId = env.RELEASE_GITHUB_APP_ID ?? '';
  const installation = env.RELEASE_GITHUB_APP_INSTALLATION_ID ?? '';
  const key = (env.RELEASE_GITHUB_APP_PRIVATE_KEY ?? '').replace(/\\n/g, '\n');
  if ([appId, installation, key].includes('')) throw new GitHubError(503, 'release GitHub App credentials are not configured');
  return { appId, installation, key };
}

async function installationToken(): Promise<string> {
  if (token && token.expires - Date.now() > TOKEN_MARGIN_MS) return token.value;
  const { appId, installation, key } = appCredentials();
  const res = await fetch(`${API}/app/installations/${installation}/access_tokens`, {
    method: 'POST',
    headers: { authorization: `Bearer ${appJwt(appId, key)}`, accept: 'application/vnd.github+json' }
  });
  if (!res.ok) throw new GitHubError(res.status, `mint installation token: ${res.status}`);
  const body = (await res.json()) as { token: string; expires_at: string };
  token = { value: body.token, expires: Date.parse(body.expires_at) };
  return token.value;
}

type Init = { method?: 'GET' | 'POST' | 'PATCH'; body?: unknown };

export async function github<T>(path: string, init: Init = {}): Promise<T> {
  const res = await fetch(`${API}/repos/${REPO}${path}`, {
    method: init.method ?? 'GET',
    headers: {
      authorization: `Bearer ${await installationToken()}`,
      accept: 'application/vnd.github+json',
      'x-github-api-version': '2022-11-28'
    },
    body: init.body === undefined ? undefined : JSON.stringify(init.body)
  });
  if (!res.ok) throw new GitHubError(res.status, `GitHub ${init.method ?? 'GET'} ${path}: ${res.status}`);
  if (res.status === 202 || res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export async function githubMaybe<T>(path: string): Promise<T | null> {
  try {
    return await github<T>(path);
  } catch (e) {
    if (e instanceof GitHubError && e.status === 404) return null;
    throw e;
  }
}
