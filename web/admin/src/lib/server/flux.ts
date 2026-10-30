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

function mergePatch(name: string, body: unknown): Promise<Kustomization> {
  return kube(`${KUSTOMIZATIONS}/${encodeURIComponent(name)}`, {
    method: 'PATCH',
    body,
    contentType: 'application/merge-patch+json'
  });
}

export async function reconcile(name: string): Promise<void> {
  await mergePatch(name, { metadata: { annotations: { 'reconcile.fluxcd.io/requestedAt': new Date().toISOString() } } });
}

export async function setSuspended(name: string, suspend: boolean): Promise<void> {
  await mergePatch(name, { spec: { suspend } });
}

// A merge patch replaces the whole images list: the resourceVersion makes a concurrent edit fail instead of vanish.
async function editImages(tier: string, edit: (images: ImageOverride[]) => ImageOverride[]): Promise<void> {
  const k = await kube<Kustomization>(`${KUSTOMIZATIONS}/${encodeURIComponent(tier)}`);
  await mergePatch(tier, {
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

function tierFor(service: string): string {
  const tier = SERVICE_TIER[service];
  if (!tier) throw new FluxError(400, `unknown service ${service}`);
  return tier;
}

function imageName(service: string): string {
  return `${IMAGE_REPO}/${service}`;
}

export function serviceOf(image: string): string {
  return image.startsWith(`${IMAGE_REPO}/`) ? image.slice(IMAGE_REPO.length + 1) : image;
}

export async function pinImage(service: string, tag: string): Promise<ImageOverride> {
  const tier = tierFor(service);
  const pin = { name: imageName(service), newTag: tag, digest: await resolveDigest(service, tag) };
  await editImages(tier, (images) => [...images.filter((i) => i.name !== pin.name), pin]);
  return pin;
}

export async function clearImage(service: string): Promise<void> {
  await editImages(tierFor(service), (images) => images.filter((i) => i.name !== imageName(service)));
}

const MANIFEST_TYPES = [
  'application/vnd.oci.image.index.v1+json',
  'application/vnd.docker.distribution.manifest.list.v2+json',
  'application/vnd.oci.image.manifest.v1+json'
].join(', ');

// The manifests pin tag@digest; overriding only the tag would keep the old digest, which wins at pull.
async function resolveDigest(service: string, tag: string): Promise<string> {
  const repo = `adamousmer/itsbagelbot/${service}`;
  const auth = await fetch(`https://ghcr.io/token?scope=repository:${repo}:pull`);
  if (!auth.ok) throw new FluxError(502, `ghcr token for ${service}: ${auth.status}`);
  const { token } = (await auth.json()) as { token: string };
  const res = await fetch(`https://ghcr.io/v2/${repo}/manifests/${encodeURIComponent(tag)}`, {
    method: 'HEAD',
    headers: { authorization: `Bearer ${token}`, accept: MANIFEST_TYPES }
  });
  const digest = res.headers.get('docker-content-digest');
  if (!res.ok || !digest) throw new FluxError(404, `${service}:${tag} is not in ghcr`);
  return digest;
}
