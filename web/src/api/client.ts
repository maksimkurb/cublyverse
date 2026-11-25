import createClient from "openapi-fetch";

import { createAuthMiddleware } from "./authMiddleware";
import type { components, paths } from "./schema";

/** Payload types generated from `api/openapi.yaml`. */
export type Me = components["schemas"]["Me"];
export type Reminder = components["schemas"]["Reminder"];
export type ReminderOccurrence = components["schemas"]["ReminderOccurrence"];
export type CreateReminderRequest = components["schemas"]["CreateReminderRequest"];
export type UpdateReminderRequest = components["schemas"]["UpdateReminderRequest"];
export type TelegramAuthResponse = components["schemas"]["TelegramAuthResponse"];
export type APIToken = components["schemas"]["APIToken"];
export type Connection = components["schemas"]["Connection"];
export type APITokenScope = components["schemas"]["APITokenScope"];
export type CreateTokenRequest = components["schemas"]["CreateTokenRequest"];
export type CreatedToken = components["schemas"]["CreatedToken"];
export type ApiError = components["schemas"]["Error"];

/** Fetch implementation accepted by the client factory (injectable in tests). */
export type FetchLike = (request: Request) => Promise<Response>;

/**
 * Builds a typed API client.
 *
 * URLs are relative to the page origin: the Go binary serves the WebApp and the
 * API together, and the Vite dev server proxies `/api` to the backend.
 */
export function createApiClient(fetchImpl: FetchLike = (request) => globalThis.fetch(request), baseUrl = "/api/v1") {
  const client = createClient<paths>({ baseUrl, fetch: fetchImpl });
  client.use(createAuthMiddleware(fetchImpl));
  return client;
}

/** Typed client for the Cublyverse HTTP API. */
export const api = createApiClient();
