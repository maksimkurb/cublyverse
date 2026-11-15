import { describe, expect, it } from "vitest";

import { makeToken } from "../../test/fixtures";
import {
  buildCreateTokenRequest,
  copyToken,
  emptyTokenForm,
  expiryLabel,
  formatTokenDate,
  prefixLabel,
  scopesLabel,
  tokenState,
  tokenStateLabel,
} from "./tokens";

describe("tokenState", () => {
  it("treats a live token as active", () => {
    expect(tokenState(makeToken(), new Date("2026-01-20T00:00:00Z"))).toBe("active");
  });

  it("reports a revoked token even if it also expired", () => {
    const token = makeToken({ revoked_at: "2026-01-18T00:00:00Z", expires_at: "2026-01-01T00:00:00Z" });
    expect(tokenState(token, new Date("2026-01-20T00:00:00Z"))).toBe("revoked");
    expect(tokenStateLabel("revoked")).toBe("Отозван");
  });

  it("reports an expired token", () => {
    const token = makeToken({ expires_at: "2026-01-01T00:00:00Z" });
    expect(tokenState(token, new Date("2026-01-20T00:00:00Z"))).toBe("expired");
    expect(tokenStateLabel("expired")).toBe("Истёк");
  });

  it("keeps a token without expiry active", () => {
    expect(tokenState(makeToken({ expires_at: null }))).toBe("active");
  });
});

describe("labels", () => {
  it("formats the prefix the way the list shows it", () => {
    expect(prefixLabel("9xQ2mB7k")).toBe("cv_pat_9xQ2mB7k…");
  });

  it("translates scopes and keeps unknown ones visible", () => {
    expect(scopesLabel(["profile:read", "reminders:write"])).toBe("Профиль, Изменение напоминаний");
    expect(scopesLabel([])).toBe("Без областей доступа");
  });

  it("formats dates and the expiry line", () => {
    expect(formatTokenDate("2026-01-15T09:30:00Z")).toMatch(/15\.01\.2026/);
    expect(formatTokenDate(null)).toBe("—");
    expect(formatTokenDate("not a date")).toBe("—");
    expect(expiryLabel(makeToken({ expires_at: null }))).toBe("Бессрочно");
    expect(expiryLabel(makeToken({ expires_at: "2026-04-15T09:30:00Z" }))).toMatch(/^до 15\.04\.2026$/);
  });
});

describe("buildCreateTokenRequest", () => {
  it("accepts the default form", () => {
    const result = buildCreateTokenRequest({ ...emptyTokenForm(), name: "  Claude Code  " });
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.body.name).toBe("Claude Code");
      expect(result.body.expires_in_days).toBeNull();
      expect(result.body.scopes).toEqual(["profile:read", "reminders:read", "reminders:write"]);
    }
  });

  it("passes the selected lifetime", () => {
    const result = buildCreateTokenRequest({ name: "script", scopes: ["reminders:read"], expiry: "30" });
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.body.expires_in_days).toBe(30);
    }
  });

  it("rejects an empty name", () => {
    const result = buildCreateTokenRequest({ ...emptyTokenForm(), name: "   " });
    expect(result).toEqual({ ok: false, message: "Название должно быть от 1 до 80 символов" });
  });

  it("rejects a name longer than 80 characters", () => {
    const result = buildCreateTokenRequest({ ...emptyTokenForm(), name: "x".repeat(81) });
    expect(result.ok).toBe(false);
  });

  it("requires at least one scope", () => {
    const result = buildCreateTokenRequest({ name: "x", scopes: [], expiry: "" });
    expect(result).toEqual({ ok: false, message: "Выберите хотя бы одну область доступа" });
  });

  it("rejects an out-of-range lifetime", () => {
    const result = buildCreateTokenRequest({ name: "x", scopes: ["reminders:read"], expiry: "0" });
    expect(result).toEqual({ ok: false, message: "Срок действия должен быть от 1 до 365 дней" });
  });
});

describe("copyToken", () => {
  it("returns false when the clipboard is unavailable", async () => {
    const original = Object.getOwnPropertyDescriptor(navigator, "clipboard");
    Object.defineProperty(navigator, "clipboard", { value: undefined, configurable: true });
    try {
      await expect(copyToken("cv_pat_x")).resolves.toBe(false);
    } finally {
      if (original) {
        Object.defineProperty(navigator, "clipboard", original);
      }
    }
  });
});
