import { beforeEach, describe, expect, it, vi } from "vitest";

import { createApiClient } from "./client";
import {
  getSessionToken,
  resetSessionState,
  setSessionRefresher,
  setSessionToken,
  setUnauthorizedHandler,
} from "../auth/session";

const baseUrl = "http://localhost/api/v1";

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const me = { id: "user-1", timezone: "UTC", created_at: "2026-01-15T09:30:00Z", bindings: [] };

function sentRequest(fetchImpl: { mock: { calls: unknown } }, index = 0): Request {
  const calls = fetchImpl.mock.calls as Array<[Request]>;
  return calls[index][0];
}

beforeEach(() => {
  resetSessionState();
});

describe("API client auth middleware", () => {
  it("attaches the in-memory Bearer token", async () => {
    setSessionToken("memory-token");
    const fetchImpl = vi.fn(async () => jsonResponse(200, me));
    const client = createApiClient(fetchImpl, baseUrl);

    await client.GET("/me");

    const request = sentRequest(fetchImpl);
    expect(request.headers.get("Authorization")).toBe("Bearer memory-token");
  });

  it("re-authenticates once on 401 and retries with the fresh token", async () => {
    setSessionToken("stale-token");
    const refresher = vi.fn().mockResolvedValue("fresh-token");
    setSessionRefresher(refresher);
    const fetchImpl = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(401, { code: "unauthorized", message: "Требуется вход" }))
      .mockResolvedValueOnce(jsonResponse(200, me));
    const client = createApiClient(fetchImpl, baseUrl);

    const { data } = await client.GET("/me");

    expect(data?.id).toBe("user-1");
    expect(refresher).toHaveBeenCalledTimes(1);
    expect(fetchImpl).toHaveBeenCalledTimes(2);
    const retry = sentRequest(fetchImpl, 1);
    expect(retry.headers.get("Authorization")).toBe("Bearer fresh-token");
    expect(getSessionToken()).toBe("fresh-token");
  });

  it("retries at most once", async () => {
    setSessionToken("stale-token");
    setSessionRefresher(vi.fn().mockResolvedValue("fresh-token"));
    const fetchImpl = vi
      .fn()
      .mockResolvedValue(jsonResponse(401, { code: "unauthorized", message: "Требуется вход" }));
    const client = createApiClient(fetchImpl, baseUrl);

    const { response } = await client.GET("/me");

    expect(response.status).toBe(401);
    expect(fetchImpl).toHaveBeenCalledTimes(2);
  });

  it("reports the lost session when the refresh fails and does not loop", async () => {
    setSessionToken("stale-token");
    setSessionRefresher(vi.fn().mockResolvedValue(null));
    const unauthorized = vi.fn();
    setUnauthorizedHandler(unauthorized);
    const fetchImpl = vi.fn().mockResolvedValue(jsonResponse(401, { code: "unauthorized", message: "Требуется вход" }));
    const client = createApiClient(fetchImpl, baseUrl);

    const { response } = await client.GET("/me");

    expect(response.status).toBe(401);
    expect(fetchImpl).toHaveBeenCalledTimes(1);
    expect(unauthorized).toHaveBeenCalledTimes(1);
  });

  it("reports a browser 401 without a token once", async () => {
    const unauthorized = vi.fn();
    setUnauthorizedHandler(unauthorized);
    const fetchImpl = vi.fn().mockResolvedValue(jsonResponse(401, { code: "unauthorized", message: "Требуется вход" }));
    const client = createApiClient(fetchImpl, baseUrl);

    await client.GET("/me");

    expect(fetchImpl).toHaveBeenCalledTimes(1);
    expect(unauthorized).toHaveBeenCalledTimes(1);
  });

  it("adds the JSON content type to bodyless mutating requests", async () => {
    const fetchImpl = vi.fn(async () => new Response(null, { status: 204 }));
    const client = createApiClient(fetchImpl, baseUrl);

    await client.DELETE("/reminders/{id}", { params: { path: { id: "r-1" } } });

    const request = sentRequest(fetchImpl);
    expect(request.headers.get("Content-Type")).toBe("application/json");
  });
});
