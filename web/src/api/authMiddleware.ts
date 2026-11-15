import type { Middleware } from "openapi-fetch";

import { getSessionToken, notifyUnauthorized, refreshSessionToken } from "../auth/session";

/** Methods that change server state and need a JSON content type with cookies. */
const MUTATING = new Set(["POST", "PUT", "PATCH", "DELETE"]);

/**
 * Request middleware of the API client.
 *
 * - attaches the in-memory Bearer token, if any;
 * - declares `Content-Type: application/json` for bodyless mutating requests,
 *   because the server rejects a cookie-authenticated `DELETE`/`POST` without
 *   it (CSRF defence);
 * - on `401` with a Bearer token, re-runs the Telegram sign-in exactly once and
 *   repeats the request with the fresh token.
 *
 * The request clone is made before `fetch` consumes the body, so the retry can
 * replay a JSON body safely.
 */
export function createAuthMiddleware(fetchImpl: (request: Request) => Promise<Response>): Middleware {
  const replayable = new WeakMap<Request, Request>();

  return {
    onRequest({ request }) {
      const token = getSessionToken();
      if (token) {
        request.headers.set("Authorization", `Bearer ${token}`);
      }
      if (MUTATING.has(request.method.toUpperCase()) && !request.headers.has("Content-Type")) {
        request.headers.set("Content-Type", "application/json");
      }
      replayable.set(request, request.clone());
    },

    async onResponse({ request, response }) {
      const clone = replayable.get(request);
      replayable.delete(request);
      if (response.status !== 401) {
        return;
      }

      if (getSessionToken()) {
        const fresh = await refreshSessionToken();
        if (fresh && clone) {
          clone.headers.set("Authorization", `Bearer ${fresh}`);
          return fetchImpl(clone);
        }
      }
      notifyUnauthorized();
    },
  };
}
