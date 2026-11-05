// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Chk } from './check';
import { JSON_PATH_MAX_DEPTH, PATH_SEGMENT_RE, validateFetchDef } from '../engine/fetch-validate';
import { checkVarIdentity, counterName } from './conditions';
import { register, type Table, type Wide } from './engine';
import {
  CODE,
  MAX_DELAY_MS,
  MAX_FETCH_URL_LENGTH,
  MAX_REWARD_COST,
  MAX_TEMPLATE_LENGTH,
  MAX_VARIABLE_AMOUNT,
  RECIPE_NAME_PATTERN,
} from './limits';
import type * as T from './types';

const ANNOUNCE_COLORS = ['', 'primary', 'blue', 'green', 'orange', 'purple'];
const COMMERCIAL_LENGTHS = [30, 60, 90, 120, 150, 180];
const LIGHTS_MODES = ['color', 'scene'];
const LIGHTS_COLOR_PATTERN = /^#[0-9a-fA-F]{6}$/;
const MAX_AMOUNT =1000000;
const MAX_SET_AMOUNT = 1000000000;
const MAX_TIMEOUT_SECONDS = 1209600;

const checkText = (c: Chk, p: Wide<T.TextAction>) => void c.template('text', p.text);

function checkAnnounce(c: Chk, p: Wide<T.AnnounceAction>): void {
  c.template('text', p.text);
  if (!ANNOUNCE_COLORS.includes(p.color)) c.add('color', CODE.invalidEnum);
}

function checkTimeout(c: Chk, p: Wide<T.TimeoutAction>): void {
  c.login('login', p.login, 1);
  c.inRange('seconds', p.seconds, 1, MAX_TIMEOUT_SECONDS);
  c.reason('reason', p.reason, 0);
}

function checkBan(c: Chk, p: Wide<T.BanAction>): void {
  c.login('login', p.login, 1);
  c.reason('reason', p.reason, 0);
}

function checkWarn(c: Chk, p: Wide<T.WarnAction>): void {
  c.login('login', p.login, 1);
  c.reason('reason', p.reason, 1);
}

function checkCommercial(c: Chk, p: Wide<T.CommercialAction>): void {
  if (!COMMERCIAL_LENGTHS.includes(p.seconds)) c.add('seconds', CODE.invalidEnum);
}

function checkPointsAdd(c: Chk, p: Wide<T.PointsAddAction>): void {
  c.login('login', p.login, 0);
  c.inRange('amount', p.amount, -MAX_AMOUNT, MAX_AMOUNT);
  c.nonZero('amount', p.amount);
}

function checkPointsSet(c: Chk, p: Wide<T.PointsSetAction>): void {
  c.login('login', p.login, 0);
  c.inRange('amount', p.amount, 0, MAX_SET_AMOUNT);
}

function checkCounterAdd(c: Chk, p: Wide<T.CounterAddAction>): void {
  counterName(c, 'name', p.name);
  c.inRange('amount', p.amount, -MAX_AMOUNT, MAX_AMOUNT);
  c.nonZero('amount', p.amount);
}

function fetchUrlRejected(url: string): boolean {
  return validateFetchDef({ name: 'a', url, kind: 'plain', path: [], keyLabel: '' }).url !== undefined;
}

export function checkSaveAs(c: Chk, name: string): void {
  if (name !== '') c.format('save_as', name, RECIPE_NAME_PATTERN);
}

export function checkHttpPostFields(c: Chk, p: { url: string; body: string; save_as: string }): void {
  if (c.text('url', p.url, 1, MAX_FETCH_URL_LENGTH) && fetchUrlRejected(p.url)) c.add('url', CODE.invalidFormat);
  c.text('body', p.body, 0, 2000);
  checkSaveAs(c, p.save_as);
}

function jsonPathRejected(path: string[]): boolean {
  return path.length > JSON_PATH_MAX_DEPTH || !path.every((segment) => PATH_SEGMENT_RE.test(segment));
}

function httpsTemplateRejected(template: string): boolean {
  return !template.startsWith('{') && !template.startsWith('https://');
}

function fetchTemplateRejected(url: string): boolean {
  return url.includes('{') ? httpsTemplateRejected(url) : fetchUrlRejected(url);
}

export function checkHttpGetFields(c: Chk, p: { url: string; json_path: string[]; save_as: string }): void {
  if (c.text('url', p.url, 1, MAX_FETCH_URL_LENGTH) && fetchTemplateRejected(p.url)) c.add('url', CODE.invalidFormat);
  if (jsonPathRejected(p.json_path)) c.add('json_path', CODE.invalidFormat);
  checkSaveAs(c, p.save_as);
}

const TEXT_OPS = ['upper', 'lower', 'trim', 'length', 'replace', 'substring', 'split_pick', 'contains', 'url_encode'];

function checkTextStep(c: Chk, p: Wide<T.TextStepAction>): void {
  c.oneOf('op', p.op, TEXT_OPS);
  c.text('input', p.input, 0, MAX_TEMPLATE_LENGTH);
  c.text('arg1', p.arg1, 0, MAX_TEMPLATE_LENGTH);
  c.text('arg2', p.arg2, 0, MAX_TEMPLATE_LENGTH);
  c.format('save_as', p.save_as, RECIPE_NAME_PATTERN);
}

function checkMath(c: Chk, p: Wide<T.MathAction>): void {
  c.template('expression', p.expression);
  c.format('save_as', p.save_as, RECIPE_NAME_PATTERN);
}

const REWARD_ID_PATTERN = /^[A-Za-z0-9-]+$/;

type RewardUpdateFields = { reward_id: string; enabled: boolean | null; cost: number | null };

function checkRewardUpdate(c: Chk, p: RewardUpdateFields): void {
  if (c.text('reward_id', p.reward_id, 1, 64) && !REWARD_ID_PATTERN.test(p.reward_id)) c.add('reward_id', CODE.invalidFormat);
  if (p.enabled === null && p.cost === null) c.add('enabled', CODE.required);
  if (p.cost !== null) c.inRange('cost', p.cost, 1, MAX_REWARD_COST);
}

function jsonParses(text: string): boolean {
  try {
    JSON.parse(text);
    return true;
  } catch {
    return false;
  }
}

function checkLightsScene(c: Chk, p: Wide<T.LightsAction>): void {
  c.text('scene_id', p.scene_id, 0, 100);
  c.text('scene_name', p.scene_name, 0, 100);
  c.text('scene_type', p.scene_type, 1, 64);
  c.text('scene_instance', p.scene_instance, 1, 64);
  if (c.text('scene_value', p.scene_value, 1, 200) && !jsonParses(p.scene_value)) c.add('scene_value', CODE.invalidFormat);
}

function checkLights(c: Chk, p: Wide<T.LightsAction>): void {
  c.text('device', p.device, 1, 64);
  c.text('sku', p.sku, 1, 32);
  c.text('device_name', p.device_name, 0, 100);
  c.oneOf('mode', p.mode, LIGHTS_MODES);
  if (p.mode === 'scene') checkLightsScene(c, p);
  else c.format('color', p.color, LIGHTS_COLOR_PATTERN);
}

const noop = () => {};
const login = register<Wide<T.LoginAction>>({ login: 'string' }, (c, p) => void c.login('login', p.login, 1));
const empty = register<Record<string, never>>({}, noop);
const text = register<Wide<T.TextAction>>({ text: 'string' }, checkText);

export const SIMPLE_ACTIONS: Table = {
  chat: text,
  announce: register<Wide<T.AnnounceAction>>({ text: 'string', color: 'string' }, checkAnnounce),
  reply: text,
  timeout: register<Wide<T.TimeoutAction>>({ login: 'string', seconds: 'int', reason: 'string' }, checkTimeout),
  ban: register<Wide<T.BanAction>>({ login: 'string', reason: 'string' }, checkBan),
  delete_message: empty,
  warn: register<Wide<T.WarnAction>>({ login: 'string', reason: 'string' }, checkWarn),
  shield: register<Wide<T.ShieldAction>>({ on: 'bool' }, noop),
  shoutout: login,
  clip: empty,
  marker: register<Wide<T.MarkerAction>>({ description: 'string' }, (c, p) => void c.text('description', p.description, 0, 140)),
  set_title: register<Wide<T.SetTitleAction>>({ title: 'string' }, (c, p) => void c.text('title', p.title, 1, 140)),
  set_game: register<Wide<T.SetGameAction>>({ game: 'string' }, (c, p) => void c.text('game', p.game, 1, 100)),
  set_tags: register<Wide<T.SetTagsAction>>({ tags: 'string' }, (c, p) => void c.template('tags', p.tags)),
  commercial: register<Wide<T.CommercialAction>>({ seconds: 'int' }, checkCommercial),
  points_add: register<Wide<T.PointsAddAction>>({ login: 'string', amount: 'int' }, checkPointsAdd),
  points_set: register<Wide<T.PointsSetAction>>({ login: 'string', amount: 'int' }, checkPointsSet),
  counter_add: register<Wide<T.CounterAddAction>>({ name: 'string', amount: 'int' }, checkCounterAdd),
  counter_set: register<Wide<T.CounterSetAction>>({ name: 'string', value: 'int' }, (c, p) => counterName(c, 'name', p.name)),
  redemption: register<Wide<T.RedemptionAction>>({ status: 'string' }, (c, p) => c.oneOf('status', p.status, ['fulfill', 'cancel'])),
  delay: register<Wide<T.DelayAction>>({ ms: 'int' }, (c, p) => c.inRange('ms', p.ms, 1, MAX_DELAY_MS)),
  run: register<Wide<T.RunAction>>({ automation_id: 'string' }, (c, p) => void c.text('automation_id', p.automation_id, 1, 64)),
  var_set: register<Wide<T.VarSetAction>>({ scope: 'string', name: 'string', value: 'string' }, (c, p) => {
    checkVarIdentity(c, p);
    c.text('value', p.value, 0, MAX_TEMPLATE_LENGTH);
  }),
  var_add: register<Wide<T.VarAddAction>>({ scope: 'string', name: 'string', amount: 'int' }, (c, p) => {
    checkVarIdentity(c, p);
    c.inRange('amount', p.amount, -MAX_VARIABLE_AMOUNT, MAX_VARIABLE_AMOUNT);
    c.nonZero('amount', p.amount);
  }),
  var_delete: register<Wide<T.VarDeleteAction>>({ scope: 'string', name: 'string' }, checkVarIdentity),
  text: register<Wide<T.TextStepAction>>({ op: 'string', input: 'string', arg1: 'string', arg2: 'string', save_as: 'string' }, checkTextStep),
  math: register<Wide<T.MathAction>>({ expression: 'string', save_as: 'string' }, checkMath),
  reward_update: register<RewardUpdateFields>(
    { reward_id: 'string', enabled: 'ptrbool', cost: 'ptrint', reset_on_stream_end: 'bool' },
    checkRewardUpdate,
  ),
  lights: register<Wide<T.LightsAction>>(
    {
      device: 'string',
      sku: 'string',
      device_name: 'string',
      mode: 'string',
      color: 'string',
      scene_id: 'string',
      scene_name: 'string',
      scene_type: 'string',
      scene_instance: 'string',
      scene_value: 'string',
    },
    checkLights,
    { mode: 'color' },
  ),
  stop: empty,
};

