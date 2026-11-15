import { describe, expect, it, vi } from "vitest";

import { signInWithTelegram, TelegramSignInError } from "./telegram";

const baseUrl = "http://localhost/api/v1";

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("signInWithTelegram", () => {
  it("posts initData and returns the session", async () => {
    const fetchImpl = vi.fn(async () =>
      jsonResponse(200, {
        status: "ok",
        token: "secret-token",
        expires_at: "2026-01-16T09:30:00Z",
        user: { id: "user-1", timezone: "UTC", created_at: "2026-01-15T09:30:00Z", bindings: [] },
      }),
    );

    const result = await signInWithTelegram({ initData: "query_id=1&hash=abc", fetchImpl, baseUrl });

    expect(result.status).toBe("ok");
    expect(result.token).toBe("secret-token");
    expect(fetchImpl).toHaveBeenCalledTimes(1);

    const calls = fetchImpl.mock.calls as unknown as Array<[Request]>;
    const request = calls[0][0];
    expect(request.url).toBe(`${baseUrl}/auth/telegram`);
    expect(request.method).toBe("POST");
    expect(await request.json()).toEqual({ init_data: "query_id=1&hash=abc" });
  });

  it("returns the binding link when the account is not linked", async () => {
    const fetchImpl = vi.fn(async () =>
      jsonResponse(200, { status: "link_required", link_url: "/auth/login?state=abc" }),
    );

    const result = await signInWithTelegram({ initData: "hash=abc", fetchImpl, baseUrl });

    expect(result.status).toBe("link_required");
    expect(result.link_url).toBe("/auth/login?state=abc");
  });

  it("surfaces the server error message", async () => {
    const fetchImpl = vi.fn(async () =>
      jsonResponse(401, { code: "invalid_init_data", message: "Не удалось проверить данные Telegram" }),
    );

    await expect(signInWithTelegram({ initData: "bad", fetchImpl, baseUrl })).rejects.toEqual(
      expect.objectContaining({ message: "Не удалось проверить данные Telegram" }),
    );
  });

  it("rejects empty initData without a request", async () => {
    const fetchImpl = vi.fn();
    await expect(signInWithTelegram({ initData: "  ", fetchImpl, baseUrl })).rejects.toBeInstanceOf(
      TelegramSignInError,
    );
    expect(fetchImpl).not.toHaveBeenCalled();
  });
});
