// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { displayName, eventAmount, eventKey, isAlert, renderTemplate, tokens } from './events';
import type { AlertKind, Counts, FeedEvent, FeedStats, OverlayEvent } from './feed';
import { mergeGiftBursts } from './gift-burst';
import { applyFont, changed, node, timed, type Install } from './view';
import type { CounterField, Direction, LabelSource, Period, WidgetSettings } from './widget-types';

type Timed = { now: number; fade: number };
export function recent(items: FeedEvent[], time: Timed, count: number, direction: Direction): FeedEvent[] {
  const start = time.fade === 0 ? -Infinity : time.now - time.fade * 1000;
  const result = items.filter(item => Date.parse(item.at) >= start).slice(-count);
  return direction === 'newest_first' ? result.reverse() : result;
}

export function track(items: FeedEvent[], event: FeedEvent, limit: number): FeedEvent[] {
  const key = eventKey(event);
  return [...items.filter(item => eventKey(item) !== key), event].slice(-limit);
}

function eventLine(event: FeedEvent, settings: WidgetSettings['eventlist']): string[] {
  if (!isAlert(event.kind)) return [];
  const rule = settings.rules[event.kind];
  return rule.enabled && eventAmount(event) >= rule.minAmount ? [renderTemplate(rule.template, tokens(event))] : [];
}

export const installEventList: Install<'eventlist'> = (host, settings, { assets }) => {
  applyFont(host, settings.style, assets);
  let transient: FeedEvent[] = [];
  let previous: FeedEvent[] | null | undefined;
  let merged: FeedEvent[] | undefined;
  return {
    event: event => {
      if (!event.test || event.replay) return;
      transient = track(transient, { ...event, at: new Date().toISOString() }, settings.count);
      merged = undefined;
    },
    ...timed(changed(({ snapshot, now }) => {
      if (snapshot.events !== previous || !merged) {
        previous = snapshot.events;
        merged = mergeGiftBursts([...(previous ?? []), ...transient]);
      }
      const eligible = merged.filter(event => eventLine(event, settings).length);
      return recent(eligible, { now, fade: settings.fade }, settings.count, settings.direction).flatMap(event => eventLine(event, settings));
    }, lines => host.replaceChildren(...lines.map(line => node('div', 'obs-event-line', line)))), 250),
  };
};

function subPoints(stats: FeedStats, weights: WidgetSettings['goal']['weights']): number {
  const tiers = stats.session?.tiers;
  if (!tiers) return stats.session?.sub_points ?? 0;
  return Object.entries(weights).reduce((total, [tier, weight]) => total + weight * (tiers[tier as keyof typeof tiers] ?? 0), 0);
}

type Goal = WidgetSettings['goal'];
const counted = /^(session|week|month|all_time)_(follows|subs|gifts|bits)$/;

function metricValue(settings: Goal, stats: FeedStats): number {
  if (settings.metric === 'manual') return settings.manual;
  if (settings.metric === 'sub_points') return subPoints(stats, settings.weights);
  const [, period, field] = counted.exec(settings.metric) ?? [];
  return stats[period as Period]?.[field as CounterField] ?? 0;
}

export const installGoal: Install<'goal'> = (host, settings, { assets }) => {
  applyFont(host, settings.style, assets);
  const meter = node('div', `obs-goal obs-goal-${settings.display}`);
  const label = node('span', 'obs-goal-title', settings.title);
  const value = node('span', 'obs-goal-value');
  const track = node('div', 'obs-goal-track');
  track.append(node('div', 'obs-goal-fill'));
  meter.style.setProperty('--fill', settings.fill);
  meter.style.setProperty('--track', settings.track);
  meter.append(label, track, value);
  host.append(meter);

  return { update: changed(({ snapshot }) => settings.start + metricValue(settings, snapshot.stats), count => {
    const ratio = Math.max(0, Math.min(1, count / settings.target));
    meter.style.setProperty('--progress', `${ratio * 100}%`);
    value.textContent = `${count.toLocaleString()} / ${settings.target.toLocaleString()}`;
  }) };
};

const latestKinds: Partial<Record<LabelSource, AlertKind>> = {
  latest_follow: 'follow', latest_sub: 'sub', latest_resub: 'resub', latest_cheer: 'cheer',
  latest_raid: 'raid', latest_gift: 'gift_sub', latest_redemption: 'redemption',
};
type LeaderKey = 'top_cheerer' | 'top_gifter' | 'all_time_top_cheerer' | 'all_time_top_gifter';
const leaderSources: Record<LeaderKey, true> = { top_cheerer: true, top_gifter: true, all_time_top_cheerer: true, all_time_top_gifter: true };

const periodPrefix = /^(session|week|month|all_time)_/;

function counter(source: LabelSource, stats: FeedStats): Record<string, number> {
  const period = periodPrefix.exec(source)?.[1] as Period;
  const value = stats[period]?.[source.replace(periodPrefix, '') as keyof Counts];
  const count = typeof value === 'number' ? value : 0;
  return { count, amount: count };
}

function latest(kind: AlertKind, stats: FeedStats): Record<string, string | number> | undefined {
  const event = kind === 'gift_sub' ? latestGift(stats) : stats.latest?.[kind];
  return event ? tokens(event) : undefined;
}

function latestGift(stats: FeedStats): OverlayEvent | undefined {
  const single = stats.latest?.gift_sub;
  return stats.latest?.community_gift ?? (single?.user ? single : undefined);
}

function labelData(source: LabelSource, stats: FeedStats): Record<string, string | number> | undefined {
  if (periodPrefix.test(source) && !(source in leaderSources)) return counter(source, stats);
  const kind = latestKinds[source];
  if (kind) return latest(kind, stats);
  const leader = stats[source as LeaderKey];
  return leader ? { name: displayName(leader), amount: leader.amount, count: leader.amount } : undefined;
}

export const installLabel: Install<'label'> = (host, settings, { assets }) => {
  applyFont(host, settings.style, assets);
  const line = node('div', 'obs-label');
  host.append(line);
  const measure = () => line.classList.toggle('obs-marquee', settings.marquee && line.scrollWidth > host.clientWidth);
  const resize = new ResizeObserver(measure);
  resize.observe(host);
  return {
    update: changed(({ snapshot }) => labelData(settings.source, snapshot.stats), data => {
      line.textContent = data ? renderTemplate(settings.template, data) : settings.empty;
      measure();
    }),
    dispose: () => resize.disconnect(),
  };
};