// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { defineRead, defineWrite } from '@bagel/kit/server/service';
import type { PoweredConfig } from '@bagel/kit/overlays';
import { effectiveId } from '#lib/server/board';
import type { Session } from '#lib/server/session';
import { SUB } from './subjects';

export type PoweredView = {
  url: string;
  config: PoweredConfig;
  revision: number;
};

const ownerOf = (session: Session) => ({ user_id: effectiveId(session) });
const view = ({ url, config, revision }: PoweredView): PoweredView => ({ url, config, revision });

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

export const poweredGet = defineRead({
  subject: `${SUB.overlays}.powered.get`,
  request: ownerOf,
  map: view
});

export const poweredSave = defineWrite({
  subject: `${SUB.overlays}.powered.save`,
  request: (session: Session, save: { config: PoweredConfig; revision: number }) => ({ ...ownerOf(session), ...save }),
  map: view
});

export const poweredRotate = defineWrite({
  subject: `${SUB.overlays}.powered.rotate`,
  request: ownerOf,
  map: view
});
