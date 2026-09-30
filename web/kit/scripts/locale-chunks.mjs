// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const CONSOLE_FILE = /\/locales\/([\w-]+)\/console\//;

export const localeChunks = {
  /** @param {string} id */
  name: (id) => {
    const match = CONSOLE_FILE.exec(id);
    return match ? `locale-${match[1]}` : null;
  }
};
