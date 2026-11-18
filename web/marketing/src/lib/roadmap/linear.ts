// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const HORIZONS = ['now', 'next', 'later'] as const;
export type Horizon = (typeof HORIZONS)[number];
export const COLUMNS = [...HORIZONS, 'shipped'] as const;
export type Column = (typeof COLUMNS)[number];

export const PUBLIC_LABEL = 'Public';
export const HORIZON_GROUP = 'Horizon';
export const SHIPPED_WINDOW_DAYS = 90;

const LINEAR_API = 'https://api.linear.app/graphql';
const PAGE_SIZE = 100;
// Linear rejects queries above complexity 10000; unbounded labels on 100 projects scores about 11480.
const LABELS_PER_PROJECT = 25;
const DAY_MS = 86_400_000;

const QUERY = `query PublicRoadmap($after: String) {
  projects(first: ${PAGE_SIZE}, after: $after, filter: { labels: { some: { name: { eq: "${PUBLIC_LABEL}" } } } }) {
    nodes {
      slugId
      name
      description
      priority
      sortOrder
      completedAt
      status { type }
      labels(first: ${LABELS_PER_PROJECT}) { nodes { name parent { name } } }
    }
    pageInfo { hasNextPage endCursor }
  }
}`;

export interface LinearProject {
  slugId: string;
  name: string;
  description: string;
  priority: number;
  sortOrder: number;
  completedAt: string | null;
  status: { type: string };
  labels: { nodes: { name: string; parent: { name: string } | null }[] };
}

export interface RoadmapItem {
  id: string;
  title: string;
  summary: string;
}

export type Roadmap = Record<Column, RoadmapItem[]>;

interface ProjectPage {
  data?: { projects: { nodes: LinearProject[]; pageInfo: { hasNextPage: boolean; endCursor: string | null } } };
  errors?: { message: string }[];
}

function horizonOf(project: LinearProject): Horizon | undefined {
  const label = project.labels.nodes.find((node) => node.parent?.name === HORIZON_GROUP);
  const name = label?.name.toLowerCase();
  return HORIZONS.find((horizon) => horizon === name);
}

function shippedRecently(project: LinearProject, now: Date): boolean {
  if (!project.completedAt) return false;
  return now.getTime() - Date.parse(project.completedAt) <= SHIPPED_WINDOW_DAYS * DAY_MS;
}

function columnOf(project: LinearProject, now: Date): Column | undefined {
  switch (project.status.type) {
    case 'canceled':
      return undefined;
    case 'completed':
      return shippedRecently(project, now) ? 'shipped' : undefined;
    default:
      return horizonOf(project);
  }
}

function rank(project: LinearProject): number {
  return project.priority === 0 ? 5 : project.priority;
}

function byPlan(a: LinearProject, b: LinearProject): number {
  return rank(a) - rank(b) || a.sortOrder - b.sortOrder;
}

function byCompletion(a: LinearProject, b: LinearProject): number {
  return Date.parse(b.completedAt ?? '') - Date.parse(a.completedAt ?? '');
}

export function toRoadmap(projects: readonly LinearProject[], now: Date): Roadmap {
  const grouped: Record<Column, LinearProject[]> = { now: [], next: [], later: [], shipped: [] };
  for (const project of projects) {
    const column = columnOf(project, now);
    if (column) grouped[column].push(project);
  }
  grouped.now.sort(byPlan);
  grouped.next.sort(byPlan);
  grouped.later.sort(byPlan);
  grouped.shipped.sort(byCompletion);
  const item = (p: LinearProject): RoadmapItem => ({ id: p.slugId, title: p.name, summary: p.description });
  return {
    now: grouped.now.map(item),
    next: grouped.next.map(item),
    later: grouped.later.map(item),
    shipped: grouped.shipped.map(item),
  };
}

async function fetchPage(apiKey: string, after: string | null): Promise<ProjectPage> {
  const response = await fetch(LINEAR_API, {
    method: 'POST',
    headers: { 'content-type': 'application/json', authorization: apiKey },
    body: JSON.stringify({ query: QUERY, variables: { after } }),
  });
  if (!response.ok) throw new Error(`roadmap: Linear answered HTTP ${response.status}`);
  return (await response.json()) as ProjectPage;
}

export async function fetchPublicProjects(apiKey: string): Promise<LinearProject[]> {
  const projects: LinearProject[] = [];
  let after: string | null = null;
  do {
    const page = await fetchPage(apiKey, after);
    if (page.errors?.length || !page.data) {
      throw new Error(`roadmap: Linear query failed: ${page.errors?.map((e) => e.message).join('; ') ?? 'no data'}`);
    }
    projects.push(...page.data.projects.nodes);
    after = page.data.projects.pageInfo.hasNextPage ? page.data.projects.pageInfo.endCursor : null;
  } while (after);
  return projects;
}

const EMPTY: Roadmap = { now: [], next: [], later: [], shipped: [] };

let pending: Promise<Roadmap> | undefined;

export function loadRoadmap(apiKey = process.env.LINEAR_API_KEY, now = new Date()): Promise<Roadmap> {
  if (!apiKey && process.env.CF_PAGES) throw new Error('roadmap: LINEAR_API_KEY is not set for this Pages build');
  if (!apiKey) return Promise.resolve(EMPTY);
  pending ??= fetchPublicProjects(apiKey).then((projects) => toRoadmap(projects, now));
  return pending;
}
