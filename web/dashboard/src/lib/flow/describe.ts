// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { specFor, type FlowNode, type NodeSpec } from './model';

export const OPS: Record<string, string> = {
  gte: 'is at least',
  lt: 'is less than',
  eq: 'is equal to',
  contains: 'contains'
};

const ROLES: Record<string, string> = {
  everyone: 'Everyone',
  sub: 'Subscribers and up',
  vip: 'VIPs and up',
  mod: 'Moderators and up',
  lead_mod: 'Lead moderators and up',
  broadcaster: 'Only the broadcaster'
};

const SUBS: Record<string, string> = {
  any: 'Someone subscribes, resubscribes or gifts',
  new: 'Someone subscribes for the first time',
  resub: 'Someone resubscribes',
  gift: 'Someone gifts a subscription'
};

const text = (node: FlowNode, key: string) => {
  const v = node.config[key];
  return v === undefined || v === false ? '' : String(v).trim();
};

const num = (node: FlowNode, key: string) => {
  const v = Number(node.config[key]);
  return Number.isFinite(v) && v > 0 ? v : 0;
};

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

export function optionLabel(spec: NodeSpec, node: FlowNode, key: string): string {
  const field = spec.fields.find((f) => f.key === key);
  const value = text(node, key);
  if (!field || field.type !== 'select') return value;
  return field.options.find((o) => o.value === value)?.label ?? (value || field.options[0]?.label || '');
}

function triggerLine(node: FlowNode, spec: NodeSpec): string {
  switch (node.type) {
    case 'twitch.chat.command':
      return text(node, 'trigger') ? `Someone types ${text(node, 'trigger')} in chat` : '';
    case 'twitch.chat.word':
      return text(node, 'word') ? `Someone says ${text(node, 'word')} in chat` : '';
    case 'twitch.sub':
      return SUBS[text(node, 'variant')] ?? '';
    case 'twitch.cheer': {
      const bits = num(node, 'minBits');
      return bits ? `Someone cheers ${bits} bits or more` : '';
    }
    case 'twitch.raid': {
      const viewers = num(node, 'minViewers');
      return viewers ? `Raid with ${viewers}+ viewers` : '';
    }
    case 'twitch.reward': {
      const reward = optionLabel(spec, node, 'reward');
      return reward ? `${reward} is redeemed` : '';
    }
    default:
      return '';
  }
}

function conditionLine(node: FlowNode): string {
  switch (node.type) {
    case 'cond.role':
      return ROLES[text(node, 'level')] ?? '';
    case 'cond.user':
      return text(node, 'login') ? `Only ${text(node, 'login')}` : '';
    case 'cond.cooldown': {
      const seconds = num(node, 'seconds');
      if (!seconds) return '';
      return `Wait ${plural(seconds, 'second', 'seconds')} between runs${node.config.perUser ? ', per viewer' : ''}`;
    }
    case 'cond.compare': {
      const left = text(node, 'left');
      const right = text(node, 'right');
      return left && right ? `${left} ${OPS[text(node, 'op')] ?? 'is'} ${right}` : '';
    }
    case 'cond.chance': {
      const percent = num(node, 'percent');
      return percent ? `Passes ${percent}% of the time` : '';
    }
    default:
      return '';
  }
}

function replyLine(node: FlowNode, spec: NodeSpec): string {
  switch (node.type) {
    case 'reply.chat':
      return text(node, 'message');
    case 'reply.discord': {
      const channel = optionLabel(spec, node, 'channel');
      const message = text(node, 'message');
      if (!message) return channel ? `Post to ${channel}` : '';
      return channel ? `${channel}: ${message}` : message;
    }
    case 'reply.points': {
      const amount = Number(node.config.amount);
      if (!Number.isFinite(amount) || amount === 0) return '';
      return amount > 0 ? `Give ${plural(amount, 'point', 'points')}` : `Take ${plural(-amount, 'point', 'points')}`;
    }
    case 'reply.counter': {
      const counter = optionLabel(spec, node, 'counter');
      return counter ? `Add one to the ${counter} counter` : '';
    }
    case 'reply.lights': {
      const light = text(node, 'deviceName') || 'the lights';
      if (text(node, 'mode') !== 'scene') {
        const color = text(node, 'color');
        return color ? `Set ${light} to ${color}` : '';
      }
      const scene = optionLabel(spec, node, 'scene');
      return scene ? `Play the ${scene} scene on ${light}` : '';
    }
    case 'reply.wait': {
      const seconds = num(node, 'seconds');
      return seconds ? `Pause ${plural(seconds, 'second', 'seconds')}` : '';
    }
    default:
      return '';
  }
}

export function portLabel(node: FlowNode, port: { id: string; label: string }): string {
  if (node.type !== 'cond.chance') return port.label;
  const percent = num(node, 'percent');
  if (!percent) return port.label;
  return port.id === 'yes' ? `${percent}% of the time` : 'Otherwise';
}

export function describe(node: FlowNode): string {
  const spec = specFor(node.type);
  if (!spec) return '';
  const line =
    spec.kind === 'trigger' ? triggerLine(node, spec) : spec.kind === 'condition' ? conditionLine(node) : replyLine(node, spec);
  return line || spec.summary;
}
