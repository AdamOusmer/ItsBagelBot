// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { defineRead, defineWrite } from '@bagel/kit/server/service';
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
};

type CustomWire = { id: number; kind: string; name: string; revision: number; document?: ServedDocument };

const customOf = ({ id, name, revision, document }: CustomWire): CustomOverlay => ({
  id,
  name,
  revision,
  document: document ?? null
});

export const overlayGetCustom = defineRead({
  subject: `${SUB.overlays}.overlay.get`,
  request: refOf,
  map: (wire: CustomWire) => (wire.kind === 'custom' ? customOf(wire) : null)
});

export const overlaySaveCustom = defineWrite({
  subject: `${SUB.overlays}.overlay.save`,
  request: (session: Session, save: { id: number; name: string; document: OverlayDocument; revision: number }) => ({
    ...ownerOf(session),
    ...(save.id ? { id: save.id } : {}),
    kind: 'custom',
    name: save.name,
    document: save.document,
    revision: save.revision
  }),
  map: customOf
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
