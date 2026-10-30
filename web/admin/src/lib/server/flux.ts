// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readFile } from 'node:fs/promises';
import type { FluxTier, ImageOverride } from '#lib/deploys/types';

const SA = '/var/run/secrets/kubernetes.io/serviceaccount';
const API = 'https://kubernetes.default.svc';
const KUSTOMIZATIONS = '/apis/kustomize.toolkit.fluxcd.io/v1/namespaces/flux-system/kustomizations';
const IMAGE_REPO = 'ghcr.io/adamousmer/itsbagelbot';

export class FluxError extends Error {
  constructor(
    readonly status: number,
    message: string
  ) {
    super(message);
  }
}

type Condition = { type: string; status: string; reason?: string; message?: string };

type Kustomization = {
  metadata: { name: string; resourceVersion: string };
  spec: { suspend?: boolean; images?: ImageOverride[]; dependsOn?: { name: string }[] };
  status?: { conditions?: Condition[]; lastAppliedRevision?: string };
};

type KubeInit = { method?: 'GET' | 'PATCH'; body?: unknown; contentType?: string };

async function kube<T>(path: string, init: KubeInit = {}): Promise<T> {
  const [token, ca] = await Promise.all([readFile(`${SA}/token`, 'utf8'), readFile(`${SA}/ca.crt`, 'utf8')]);
  const res = await fetch(`${API}${path}`, {
    method: init.method ?? 'GET',
    headers: {
      authorization: `Bearer ${token}`,
      accept: 'application/json',
      ...(init.contentType ? { 'content-type': init.contentType } : {})
    },
    body: init.body === undefined ? undefined : JSON.stringify(init.body),
    tls: { ca }
  } as RequestInit);
  if (!res.ok) throw new FluxError(res.status, `kube ${init.method ?? 'GET'} ${path}: ${res.status}`);
  return (await res.json()) as T;
}

function tierOf(k: Kustomization): FluxTier {
  const ready = k.status?.conditions?.find((c) => c.type === 'Ready');
  return {
    name: k.metadata.name,
    ready: ready ? ready.status === 'True' : null,
    reason: ready?.reason ?? '',
    message: ready?.message ?? '',
    revision: k.status?.lastAppliedRevision ?? '',
    suspended: k.spec.suspend === true,
    dependsOn: (k.spec.dependsOn ?? []).map((d) => d.name),
    images: k.spec.images ?? []
  };
}

export async function fluxTiers(): Promise<FluxTier[]> {
  const list = await kube<{ items: Kustomization[] }>(KUSTOMIZATIONS);
  return list.items.map(tierOf).filter((t) => t.name !== 'flux-system');
}

export type TierRef = { tier: string };
export type ServiceRef = { service: string };
export type PinRequest = ServiceRef & { tag: string };

type Patch = { metadata?: Record<string, unknown>; spec?: Record<string, unknown> };

function tierPath(ref: TierRef): string {
  return `${KUSTOMIZATIONS}/${encodeURIComponent(ref.tier)}`;
}

function mergePatch(ref: TierRef, body: Patch): Promise<Kustomization> {
  return kube(tierPath(ref), { method: 'PATCH', body, contentType: 'application/merge-patch+json' });
}

export async function reconcile(ref: TierRef): Promise<void> {
  await mergePatch(ref, { metadata: { annotations: { 'reconcile.fluxcd.io/requestedAt': new Date().toISOString() } } });
}

export async function setSuspended(ref: TierRef & { suspend: boolean }): Promise<void> {
  await mergePatch(ref, { spec: { suspend: ref.suspend } });
}

// A merge patch replaces the whole images list: the resourceVersion makes a concurrent edit fail instead of vanish.
async function editImages(ref: TierRef, edit: (images: ImageOverride[]) => ImageOverride[]): Promise<void> {
  const k = await kube<Kustomization>(tierPath(ref));
  await mergePatch(ref, {
    metadata: { resourceVersion: k.metadata.resourceVersion },
    spec: { images: edit(k.spec.images ?? []) }
  });
}

// Must match the tier each manifest sits in under deploy/flux/tiers.
const SERVICE_TIER: Record<string, string> = {
  commands: 'db',
  loyalty: 'db',
  modules: 'db',
  notifications: 'db',
  'discord-data': 'db',
  projector: 'db',
  transactions: 'db',
  users: 'db',
  gossip: 'app',
  warp: 'app',
  outgress: 'app',
  sesame: 'app',
  'discord-engine': 'app',
  'discord-ingress': 'app',
  'discord-outgress': 'app',
  'twitch-ingress': 'app',
  'console-dashboard': 'app',
  'console-admin': 'app',
  clusterops: 'ops'
};

export const PINNABLE = Object.keys(SERVICE_TIER).sort();

function tierFor(ref: ServiceRef): TierRef {
  const tier = SERVICE_TIER[ref.service];
  if (!tier) throw new FluxError(400, `unknown service ${ref.service}`);
  return { tier };
}

function imageName(ref: ServiceRef): string {
  return `${IMAGE_REPO}/${ref.service}`;
}

export async function pinImage(req: PinRequest): Promise<ImageOverride> {
  const pin = { name: imageName(req), newTag: req.tag, digest: await resolveDigest(req) };
  await editImages(tierFor(req), (images) => [...images.filter((i) => i.name !== pin.name), pin]);
  return pin;
}

export async function clearImage(ref: ServiceRef): Promise<void> {
  await editImages(tierFor(ref), (images) => images.filter((i) => i.name !== imageName(ref)));
}

const MANIFEST_TYPES = [
  'application/vnd.oci.image.index.v1+json',
  'application/vnd.docker.distribution.manifest.list.v2+json',
  'application/vnd.oci.image.manifest.v1+json'
].join(', ');

// The manifests pin tag@digest; overriding only the tag would keep the old digest, which wins at pull.
async function resolveDigest(req: PinRequest): Promise<string> {
  const repo = `adamousmer/itsbagelbot/${req.service}`;
  const auth = await fetch(`https://ghcr.io/token?scope=repository:${repo}:pull`);
  if (!auth.ok) throw new FluxError(502, `ghcr token for ${req.service}: ${auth.status}`);
  const { token } = (await auth.json()) as { token: string };
  const res = await fetch(`https://ghcr.io/v2/${repo}/manifests/${encodeURIComponent(req.tag)}`, {
    method: 'HEAD',
    headers: { authorization: `Bearer ${token}`, accept: MANIFEST_TYPES }
  });
  const digest = res.ok ? res.headers.get('docker-content-digest') : null;
  if (!digest) throw new FluxError(404, `${req.service}:${req.tag} is not in ghcr`);
  return digest;
}
