// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Synthesized urlfetch definitions for the Nightbot source: how a
// $(urlfetch …) / $(customapi …) call in an imported response becomes a
// reviewed definition the runtime can resolve, and the slug rule that keeps a
// re-import landing on the same names.

import { fetchDefSlug, warnDiag } from '../validate';
import type { ImportDiagnostic, ManifestFetch } from '../types';
import { IMPORT_ITEM_CAPS } from '../types';

// FETCH_DEF_CAP rides IMPORT_ITEM_CAPS.commands for the reason the other
// parsers do: every synthesized definition serves a command in this same
// manifest, so the commands ceiling bounds it by construction and no second
// public number can drift from the mirrored server-side table.
export const FETCH_DEF_CAP = IMPORT_ITEM_CAPS.commands;

// MAX_FETCH_URL_BYTES mirrors the URL validator commit's ingestion enforces per
// definition. A longer or scheme-less URL is refused HERE — left literal with
// the standard unmapped warn — rather than synthesized into a definition that
// can only fail wholesale at save time, taking its URL out of the response text
// where it stayed visible.
const MAX_FETCH_URL_BYTES = 512;

const encoder = new TextEncoder();

export interface FetchSlotSink {
  acquire(url: string): string | null;
}

export interface FetchArgs {
  url: string;
  json: boolean;
}

// parseFetchArgs reads a $(urlfetch …) / $(customapi …) body. Nightbot's `json`
// modifier only changes how the RESPONSE is handed to a following $(eval …) —
// it carries no path of its own — so it is consumed here and reported by the
// caller instead of inventing a json_path the export never stated. A URL that
// still holds a token after translation is refused: baking literal "$(…)" or
// "{…}" text into a stored definition would make it fetch a URL nobody wrote.
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

// makeFetchSlotSink allocates definition slugs for ONE command over the
// import-level def map. Slot rule: the first distinct URL takes the bare
// fetchDefSlug('nightbot', command), the Nth (N≥2) appends _N — a legal
// ^[a-z0-9_]{1,32}$ name the fetches editor's slugifier reproduces
// byte-for-byte, so a re-import lands on identical names. The same URL twice in
// one command shares its definition (equality is byte-exact here).
export function makeFetchSlotSink(
  commandName: string,
  defs: Map<string, ManifestFetch>,
  diags: ImportDiagnostic[]
): FetchSlotSink {
  const base = fetchDefSlug('nightbot', commandName);
  const byUrl = new Map<string, string>();
  let slots = 0;
  return {
    acquire(url) {
      const known = byUrl.get(url);
      if (known !== undefined) return known;
      const key = slots === 0 ? base : `${base}_${slots + 1}`;
      if (!registerDef(defs, { name: key, url, source: 'nightbot' }, diags)) return null;
      slots++;
      byUrl.set(url, key);
      return key;
    }
  };
}

// registerDef admits one definition into the import-level map: false at the
// cap, and false with a warn when the slug is already taken — two exported
// commands normalized onto one name, where first wins (deterministic by export
// order) because the loser's tokens would silently re-point at another
// command's data source.
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
  return true;
}
