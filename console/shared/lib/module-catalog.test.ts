import { describe, expect, test } from 'bun:test';
import { MODULE_CATALOG, moduleDef, moduleDelegateSections } from './types';

describe('module catalog', () => {
  test('has unique ids', () => {
    const ids = MODULE_CATALOG.map((def) => def.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  test('folds counters into Modules delegation without an enable switch', () => {
    const counters = moduleDef('counters');
    expect(counters).toBeDefined();
    expect(counters?.href).toBe('/counters');
    expect(counters?.toggleable).toBe(false);
    expect(moduleDelegateSections(counters!)).toEqual(['modules']);
  });
});
