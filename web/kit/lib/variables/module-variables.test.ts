// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { lex } from '../engine/tmpl';
import { MODULE_VARIABLES, MODULE_VARIABLE_SPECS, MODULE_VARIABLE_SAMPLES, requiredModuleVariables } from './module-variables';

const runtimeCatalog = JSON.parse(readFileSync(join(import.meta.dir,
  '../../../../internal/domain/modulevars/catalog.json'), 'utf8'));

describe('module custom command variables', () => {
  test('dashboard reply fields match the embedded Go runtime catalogue', () => {
    const publicCatalog = MODULE_VARIABLE_SPECS.map((mod) => ({
      id: mod.id,
      groups: mod.groups.map((group) => ({
        name: group.name.toLowerCase(), fields: group.fields,
        ...(group.messageKey ? { message_key: group.messageKey } : {})
      }))
    }));
    expect(publicCatalog).toEqual(runtimeCatalog);
  });

  test('module views have unique names and fields', () => {
    for (const module of MODULE_VARIABLE_SPECS) {
      const names = module.groups.map((group) => group.name);
      expect(new Set(names).size).toBe(names.length);
      for (const group of module.groups) expect(new Set(group.fields).size).toBe(group.fields.length);
    }
  });

  test('enable requirements include condition-only module references', () => {
    expect(requiredModuleVariables('{if:valorant:rr:ranked:unavailable}').map((module) => module.head)).toEqual(['valorant']);
    expect(requiredModuleVariables('{if:valorant:rank:tier=Gold 2:yes:no} {codm:level}').map((module) => module.head)).toEqual(['codm', 'valorant']);
    expect(requiredModuleVariables('{time} {if:user:yes:no}')).toHaveLength(0);
    expect(requiredModuleVariables('{valorant:tier} {valorant:rr}')).toHaveLength(1);
  });

  test('every variable requires its own module and uses a colon namespace', () => {
    for (const variable of MODULE_VARIABLES) {
      expect(variable.requires).toBe(variable.head);
      const examples = new Set<string>();
      for (const form of variable.forms) {
        expect(form.syntax.startsWith(`{${variable.head}:`)).toBe(true);
        expect(form.example).toBe(form.syntax);
        expect(examples.has(form.example)).toBe(false);
        examples.add(form.example);
        const tokens = lex(form.example);
        expect(tokens).toHaveLength(1);
        expect(tokens[0].kind).toBe('var');
        if (tokens[0].kind === 'var') expect(tokens[0].name).toBe(variable.head);
        expect(MODULE_VARIABLE_SAMPLES[form.example]).toBe(form.output);
      }
    }
  });

  test('short raffle facts use current status and loyalty exposes native command facts', () => {
    const raffle = MODULE_VARIABLE_SPECS.find((module) => module.id === 'raffle')!;
    expect(raffle.groups.find((group) => group.fields.includes('entrants'))?.name).toBe('status');
    const loyalty = MODULE_VARIABLE_SPECS.find((module) => module.id === 'loyalty')!;
    expect(loyalty.groups.find((group) => group.name === 'balance')?.fields).toEqual(
      expect.arrayContaining(['points', 'pointsname', 'watchtime', 'duration', 'user', 'name', 'hours'])
    );
  });

  test('short facts and explicit reply views are both available', () => {
    const valorant = MODULE_VARIABLES.find((variable) => variable.head === 'valorant')!;
    const examples = valorant.forms.map((form) => form.example);
    expect(examples).toContain('{valorant:tier}');
    expect(examples).toContain('{valorant:rr}');
    expect(examples).toContain('{valorant:rank:rr}');
    expect(examples).toContain('{valorant:account:level}');
    expect(examples).toContain('{valorant:matches:count}');
    expect(examples).toContain('{valorant:shop:count}');
  });
});
