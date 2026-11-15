import createClient from "openapi-fetch";

import type { components, paths } from "./schema";

/**
 * Typed client for the Cublyverse HTTP API.
 *
 * URLs are relative to the page origin: the Go binary serves both the WebApp
 * and the API, and the Vite dev server proxies `/api` to the backend.
 */
export const api = createClient<paths>({ baseUrl: "/api/v1" });

/** Payload of `GET /health`. */
export type Health = components["schemas"]["Health"];

/**
 * Loads the backend status and build version.
 *
 * @throws Error when the request fails or the backend answers with an error.
 */
export async function fetchHealth(): Promise<Health> {
  const { data, response } = await api.GET("/health");

  if (!response.ok) {
    throw new Error(`Бэкенд ответил ошибкой ${response.status}`);
  }
  if (!data) {
    throw new Error("Бэкенд вернул пустой ответ");
  }
  return data;
}
