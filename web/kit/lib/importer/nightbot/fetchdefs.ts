// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fetchDefSlug, warnDiag } from '../validate';
import type { FetchSlugSource } from '../validate';
import type { ImportDiagnostic, ImportSource, ManifestFetch } from '../types';
import { IMPORT_ITEM_CAPS } from '../types';

export const FETCH_DEF_CAP = IMPORT_ITEM_CAPS.commands;

const MAX_FETCH_URL_BYTES = 512;

const encoder = new TextEncoder();

export interface FetchSlotSink {
  acquire(url: string): string | null;
}

export interface FetchArgs {
  url: string;
  json: boolean;
}

export function parseFetchArgs(body: string): FetchArgs | null {
  const words = body.trim().split(/\s+/).filter(Boolean);
  const json = words[0]?.toLowerCase() === 'json';
  if (json) words.shift();
  if (words.length !== 1) return null;
  const url = words[0];
  if (!usableUrl(url)) return null;
  return { url, json };
}

function usableUrl(url: string): boolean {
  if (!/^https?:\/\//i.test(url)) return false;
  if (/[${}]/.test(url)) return false;
  return encoder.encode(url).length <= MAX_FETCH_URL_BYTES;
}

const MANIFEST_SOURCE: Record<FetchSlugSource, ImportSource> = {
  se: 'streamelements',
  moobot: 'moobot',
  nightbot: 'nightbot',
  fossabot: 'fossabot',
  wizebot: 'wizebot',
  slcb: 'streamlabs_desktop'
};

export function makeFetchSlotSink(
  source: FetchSlugSource,
  commandName: string,
  defs: Map<string, ManifestFetch>,
  diags: ImportDiagnostic[]
): FetchSlotSink {
  const base = fetchDefSlug(source, commandName);
  const byUrl = new Map<string, string>();
  let slots = 0;
  return {
    acquire(url) {
      const known = byUrl.get(url);
      if (known !== undefined) return known;
      const key = slots === 0 ? base : `${base}_${slots + 1}`;
      if (!registerDef(defs, { name: key, url, source: MANIFEST_SOURCE[source] }, diags)) return null;
      slots++;
      byUrl.set(url, key);
      return key;
    }
  };
}

export function createdFetchDefMessage(def: ManifestFetch): string {
  return def.url
    ? `created fetch definition ${JSON.stringify(def.name)} for ${def.url}; review it under Commands → Fetch definitions`
    : `created fetch definition ${JSON.stringify(def.name)}; add its URL under Commands → Fetch definitions`;
}

function registerDef(
  defs: Map<string, ManifestFetch>,
  def: ManifestFetch,
  diags: ImportDiagnostic[]
): boolean {
  if (defs.has(def.name)) {
    diags.push(
      warnDiag(
        -1,
        'fetch_def_collision',
        `fetch definition ${JSON.stringify(def.name)} was already synthesized with different contents; the earlier one wins`
      )
    );
    return false;
  }
  if (defs.size >= FETCH_DEF_CAP) return false;
  defs.set(def.name, def);
  diags.push(warnDiag(-1, 'fetch_def_created', createdFetchDefMessage(def)));
  return true;
}
