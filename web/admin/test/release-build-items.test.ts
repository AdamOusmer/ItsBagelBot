// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { expect, mock, test } from 'bun:test';

const job = (name: string, status: string, conclusion: string | null = null) => ({
  name,
  status,
  conclusion,
  started_at: null,
  completed_at: null,
  html_url: `https://github.test/${name}`
});

const releaseRun = {
  id: 7,
  display_title: 'release v0.4.0-beta by itsmavey',
  status: 'in_progress',
  conclusion: null,
  created_at: '2026-10-30T00:00:00Z',
  updated_at: '2026-10-30T00:05:00Z',
  html_url: 'https://github.test/runs/7',
  head_sha: 'abc'
};

const releaseJobs = [job('preflight', 'completed', 'success'), job('build', 'in_progress')];

const imageJobs = [
  job('Select images', 'completed', 'success'),
  job('Build users Intel x86_64', 'completed', 'success'),
  job('Publish manifest users', 'completed', 'success'),
  job('Build gossip Intel x86_64', 'in_progress'),
  job('Publish manifest gossip', 'queued'),
  job('Build sesame Intel x86_64', 'completed', 'failure')
];

const calls: string[] = [];

const routes: [RegExp, unknown][] = [
  [/^\/actions\/runs\/7$/, releaseRun],
  [/^\/actions\/runs\/7\/jobs/, { jobs: releaseJobs }],
  [/^\/actions\/workflows\/publish-images\.yml\/runs/, { workflow_runs: [{ id: 99 }] }],
  [/^\/actions\/runs\/99\/jobs/, { jobs: imageJobs }]
];

const github = mock(async (path: string) => {
  calls.push(path);
  const hit = routes.find(([pattern]) => pattern.test(path));
  if (!hit) throw new Error(`unexpected GitHub call ${path}`);
  return hit[1];
});

mock.module('../src/lib/server/github', () => ({
  github,
  githubMaybe: github,
  GitHubError: class GitHubError extends Error {},
  REPO: 'o/r'
}));

const { releaseApi } = await import('../src/lib/server/release');

test('the build stage carries one item per image from the release tag build', async () => {
  const run = await releaseApi.get({ actor_id: '', run_id: '7' });
  const build = run.stages.find((s) => s.id === 'build');

  expect(calls).toContain('/actions/workflows/publish-images.yml/runs?event=push&per_page=1&branch=v0.4.0-beta');
  expect(build?.items?.map((i) => [i.key, i.state, i.progress.done, i.progress.total])).toEqual([
    ['users', 'succeeded', 2, 2],
    ['gossip', 'running', 0, 2],
    ['sesame', 'failed', 1, 1]
  ]);
  expect(build?.progress).toEqual({ done: 1, total: 3 });
});
