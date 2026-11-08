// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';

// Paired devices exchange and refresh directly with the overlay auth service.
// Never forward credentials or redirect a POST from this retired endpoint.
// server_error deliberately leaves an existing device grant intact; this route
// cannot determine whether a submitted authorization code or refresh token is valid.
export const POST: RequestHandler = () => new Response('{"error":"server_error"}', {
  status: 410,
  headers: {
    'Cache-Control': 'no-store',
    Pragma: 'no-cache',
    'Content-Type': 'application/json; charset=utf-8'
  }
});
