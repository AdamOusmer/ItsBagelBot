type Expectation = {
  origin?: string;
  callbackPath?: string;
};

function parseURL(name: string, value: string | undefined): URL {
  if (!value) throw new Error(`${name} is not set`);
  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    throw new Error(`${name} must be an absolute URL`);
  }
  if (parsed.protocol !== 'https:') throw new Error(`${name} must use https`);
  return parsed;
}

export function assertOrigin(name: string, value: string | undefined): string {
  const parsed = parseURL(name, value);
  if (parsed.pathname !== '/' || parsed.search || parsed.hash) {
    throw new Error(`${name} must be an origin only, without path/query/hash`);
  }
  return parsed.origin;
}

export function assertCallback(name: string, value: string | undefined, expected: Expectation): URL {
  const parsed = parseURL(name, value);
  if (expected.origin && parsed.origin !== expected.origin) {
    throw new Error(`${name} origin ${parsed.origin} does not match ${expected.origin}`);
  }
  if (expected.callbackPath && parsed.pathname !== expected.callbackPath) {
    throw new Error(`${name} path ${parsed.pathname} does not match ${expected.callbackPath}`);
  }
  if (parsed.search || parsed.hash) throw new Error(`${name} must not include query/hash`);
  return parsed;
}
