// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface ProjectedModule {
  name: string;
  is_enabled: boolean;
  configs?: unknown;
  revision?: number;
  account_created_at?: number;
}

type ModuleHashFields = Record<string, string>;

export function decodeModuleFields(fields: ModuleHashFields): { modules: ProjectedModule[]; projected: boolean } {
  let projected = fields['modules:projected'] === '1';
  const byName = new Map<string, ProjectedModule>();
  for (const [field, value] of Object.entries(fields)) {
    const parsed = parseModuleField(field);
    if (!parsed) continue;
    const mod = byName.get(parsed.name) ?? { name: parsed.name, is_enabled: false };
    if (parsed.suffix === 'enabled') {
      mod.is_enabled = value === '1';
      projected = true;
    } else if (parsed.suffix === 'config') {
      mod.configs = safeJson(value);
      projected = true;
    } else if (parsed.suffix === 'revision') {
      mod.revision = Number(value);
    } else if (parsed.suffix === 'account_created_at') {
      mod.account_created_at = Number(value);
    }
    byName.set(parsed.name, mod);
  }
  return { modules: [...byName.values()], projected };
}

function parseModuleField(field: string): { name: string; suffix: string } | null {
  const rest = field.startsWith('module:') ? field.slice('module:'.length) : '';
  if (!rest) return null;
  const idx = rest.lastIndexOf(':');
  if (idx < 0) return null;
  return { name: rest.slice(0, idx), suffix: rest.slice(idx + 1) };
}

function safeJson(value: string): unknown {
  try {
    return JSON.parse(value);
  } catch {
    return undefined;
  }
}
