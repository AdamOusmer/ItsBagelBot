// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { RpcError } from '@bagel/kit/server/nats';
import { defineRead, defineWrite } from '@bagel/kit/server/service';
import type { AlertControl, EventKind, Subathon } from '@bagel/kit/overlay-runtime';
import type { OverlayDocument } from '@bagel/kit/overlay-document';
import type { ServedDocument } from '@bagel/kit/overlay-editor';
import { defaultPoweredConfig, type PoweredConfig } from '@bagel/kit/overlays';
import { effectiveId } from '#lib/server/board';
import type { Session } from '#lib/server/session';
import { SUB } from './subjects';

export type OverlayView = {
  id: number;
  kind: 'system' | 'custom';
  template?: string;
  name: string;
  revision: number;
  url?: string;
};

export type PoweredSettings = {
  id: number;
  config: PoweredConfig;
  revision: number;
};

type OverlayWire = OverlayView & { document?: { config?: PoweredConfig } };

const ownerOf = (session: Session) => ({ user_id: effectiveId(session) });
const refOf = (session: Session, id: number) => ({ ...ownerOf(session), id });

export type UploadTicket = { ticket: string; max_size: number; expires: number; part_size: number; parts: number };

const ticket = ({ ticket, max_size, expires, part_size, parts }: UploadTicket): UploadTicket => ({ ticket, max_size, expires, part_size, parts });

export const assetBegin = defineWrite({
  subject: `${SUB.overlays}.assets.begin`,
  request: (session: Session, begin: { display_name: string; declared_size: number }) => ({ ...ownerOf(session), ...begin }),
  map: ticket
});

export const importBegin = defineWrite({
  subject: `${SUB.overlays}.imports.begin`,
  request: (session: Session, begin: { declared_size: number; dry_run: boolean }) => ({ ...ownerOf(session), ...begin }),
  map: ticket
});

const overlayOf = ({ id, kind, template, name, revision, url }: OverlayWire): OverlayView => ({
  id, kind, template, name, revision, url
});

const settingsOf = ({ id, revision, document }: OverlayWire): PoweredSettings => ({
  id,
  revision,
  config: document?.config ?? defaultPoweredConfig()
});

export const overlayList = defineRead({
  subject: `${SUB.overlays}.overlay.list`,
  request: ownerOf,
  map: (reply: { overlays?: OverlayWire[] }) => (reply.overlays ?? []).map(overlayOf)
});

export const overlayAddTemplate = defineWrite({
  subject: `${SUB.overlays}.overlay.add_template`,
  request: (session: Session, template: string) => ({ ...ownerOf(session), template }),
  map: overlayOf
});

export const overlayGet = defineRead({
  subject: `${SUB.overlays}.overlay.get`,
  request: refOf,
  map: settingsOf
});

export const overlaySave = defineWrite({
  subject: `${SUB.overlays}.overlay.save`,
  request: (session: Session, save: { id: number; config: PoweredConfig; revision: number }) => ({
    ...refOf(session, save.id),
    config: save.config,
    revision: save.revision
  }),
  map: settingsOf
});

export type CustomOverlay = {
  id: number;
  name: string;
  revision: number;
  document: ServedDocument | null;
  url?: string;
};

type CustomWire = { id: number; kind: string; name: string; revision: number; document?: ServedDocument; url?: string };

const customOf = ({ id, name, revision, document, url }: CustomWire): CustomOverlay => ({
  id,
  name,
  revision,
  document: document ?? null,
  url
});

export function currentOnConflict(err: unknown): CustomOverlay | null {
  if (!(err instanceof RpcError) || err.code !== 'conflict') return null;
  const wire = err.reply as Partial<CustomWire> | undefined;
  return wire?.document ? customOf(wire as CustomWire) : null;
}

export const overlayGetCustom = defineRead({
  subject: `${SUB.overlays}.overlay.get`,
  request: refOf,
  map: (wire: CustomWire) => (wire.kind === 'custom' ? customOf(wire) : null)
});

export const overlaySaveCustom = defineWrite({
  subject: `${SUB.overlays}.overlay.save`,
  request: (session: Session, save: { id: number; name: string; document: OverlayDocument; revision: number }) => ({
    ...refOf(session, save.id),
    kind: 'custom',
    name: save.name,
    document: save.document,
    revision: save.revision
  }),
  map: customOf
});

export const overlayCreateCustom = defineWrite({
  subject: `${SUB.overlays}.overlay.create_custom`,
  request: (session: Session, create: { document: OverlayDocument }) => ({ ...ownerOf(session), document: create.document }),
  map: customOf
});

export type AssetView = {
  asset_id: string;
  url: string;
  type: string;
  bytes: number;
  w: number;
  h: number;
  duration_ms: number;
  sha256: string;
  display_name?: string;
};

export const assetList = defineRead({
  subject: `${SUB.overlays}.assets.list`,
  request: ownerOf,
  map: (reply: { assets?: AssetView[] }) => reply.assets ?? []
});

export const assetDelete = defineWrite({
  subject: `${SUB.overlays}.assets.delete`,
  request: (session: Session, assetId: string) => ({ ...ownerOf(session), asset_id: assetId })
});

export type FeedAction =
  | 'pause' | 'resume' | 'mute' | 'unmute' | 'skip' | 'replay' | 'clear' | 'test'
  | 'subathon_add' | 'subathon_pause' | 'subathon_resume' | 'subathon_reset';

export type FeedControlArgs = { event_id?: string; kind?: EventKind; fields?: Record<string, unknown>; seconds?: number };

export type FeedControlState = { control: AlertControl; subathon: Subathon };

export const feedControl = defineWrite({
  subject: `${SUB.overlays}.feed.control`,
  request: (session: Session, control: { action: FeedAction; args: FeedControlArgs }) => ({
    ...ownerOf(session),
    actor: session.user_id,
    action: control.action,
    args: control.args
  }),
  map: ({ control, subathon }: FeedControlState): FeedControlState => ({ control, subathon })
});

export const overlayRotate = defineWrite({
  subject: `${SUB.overlays}.overlay.rotate`,
  request: refOf,
  map: overlayOf
});

export const overlayDelete = defineWrite({
  subject: `${SUB.overlays}.overlay.delete`,
  request: refOf
});
