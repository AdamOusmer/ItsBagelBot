// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Whether the modules a Variable's `requires` can name are on for this
// broadcaster, so VariablePalette can show "Requires <module>" as live state
// (Tag tone="error" "Off") instead of a static hint with no signal behind it.
//
// Two polarities, matching app/twitch/sesame/engine/module_gate.go:107-130 —
// a {…} response token is gated by exactly the same row as its command, so a
// missing row here must resolve the same way it does in Go, or a chip could
// read "on" in the dashboard while the engine still leaves the token literal
// in chat, or the reverse:
//
//   - BuiltinEnabled (module_gate.go:107-119): a built-in command ships
//     enabled, so a broadcaster who never touched its toggle has no row, and
//     that reads as ON ("absent is ModuleOn"). followage/accountage/uptime/
//     title/game are the five builtins a Variable's `requires` names.
//   - OptInView (module_gate.go:121-138): an opt-in module has nothing to
//     run without its row, so a missing row reads as OFF. Every other module
//     in MODULE_CATALOG (quotes, time, songqueue, loyalty, ...) is this
//     polarity.
//
// Both live in the same modules-service list (listModules); only the default
// for a missing row differs, which is what GATED_BUILTINS exists to carry.
// Imported from the '@bagel/kit/catalog' subpath rather than the top-level
// barrel: the barrel eagerly evaluates the i18n catalog loader (import.meta.
// glob), a Vite-only API this file's own bun:test suite has no bundler for —
// pulling it in from a plain .test.ts crashed not just this suite but any
// other test file that happened to import '@bagel/kit' in the same run
// (module-level throw poisons that resolved path for the whole process).
// MODULE_CATALOG lives in catalog/index.ts either way, so nothing else changes.
import { MODULE_CATALOG } from '@bagel/kit/catalog';
import { listModules, type ModuleView } from './commands-store';

/** The built-in commands a manifest Variable's `requires` names
 * (kit/lib/variables/variables.ts: followage, accountage, uptime, title,
 * game) — BuiltinEnabled polarity, missing row = on. */
const GATED_BUILTINS = ['followage', 'accountage', 'uptime', 'title', 'game'] as const;

/** Pure half: turn a modules-service read into the flag map, so a failed read
 * can fall back to exactly what "no rows at all" already means (see
 * moduleFlags below) rather than a second, hand-kept default. */
export function flagsFromRows(rows: readonly ModuleView[]): Record<string, boolean> {
  const byName = new Map(rows.map((m) => [m.name, m]));
  const flags: Record<string, boolean> = {};

  for (const m of MODULE_CATALOG) {
    // toggleable === false modules (catalog tools with no enable row) carry
    // no gate for a Variable to check; skip rather than inventing a state.
    if (m.toggleable === false) continue;
    const row = byName.get(m.id);
    flags[m.id] = row ? row.is_enabled : false;
  }

  for (const id of GATED_BUILTINS) {
    const row = byName.get(id);
    // Unconditionally true, not a catalog default: BuiltinEnabled's polarity
    // (module_gate.go:107-119) is "absent is ModuleOn" full stop, independent
    // of any per-command defaultActive flag (which answers a different
    // question — a built-in's own toggle default on the commands page, not
    // this gate's missing-row reading).
    flags[id] = row ? row.is_enabled : true;
  }

  return flags;
}

/** Every gate `flagsFromRows` can answer, as if the broadcaster had no module
 * rows at all — the fallback for a blipped read (module state must fail
 * closed for opt-in modules and open for the built-ins, exactly like a
 * genuinely missing row). */
export const DEFAULT_MODULE_FLAGS: Record<string, boolean> = flagsFromRows([]);

/** The DEMO fixture's answer: every gate this module can name, all true — a
 * demo board must never render a manufactured "Requires X · Off" tag. Built
 * from DEFAULT_MODULE_FLAGS's own key set rather than a second hand-kept id
 * list, so a module added to either catalog is covered here for free. */
export const DEMO_MODULE_FLAGS: Record<string, boolean> = Object.fromEntries(
  Object.keys(DEFAULT_MODULE_FLAGS).map((id) => [id, true])
);

export async function moduleFlags(userId: string): Promise<Record<string, boolean>> {
  return flagsFromRows(await listModules(userId));
}
