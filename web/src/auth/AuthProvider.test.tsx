import { fireEvent, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { resetSessionState } from "./session";
import { AuthProvider, type AuthDeps } from "./AuthProvider";
import type { Me } from "../api/client";
import { makeFakeHost } from "../test/fakeHost";
import { renderWithHost } from "../test/render";

const user: Me = {
  id: "user-1",
  email: "user@example.com",
  display_name: "Иван",
  timezone: "Europe/Moscow",
  created_at: "2026-01-15T09:30:00Z",
  bindings: [],
};

function deps(overrides: Partial<AuthDeps> = {}): AuthDeps {
  return {
    loadMe: vi.fn(async () => ({ kind: "ok" as const, user })),
    signInTelegram: vi.fn(async () => ({ status: "ok" as const, token: "token-1", user })),
    logout: vi.fn(async () => {}),
    ...overrides,
  };
}

function renderAuth(host = makeFakeHost(), authDeps = deps()) {
  renderWithHost(
    <AuthProvider deps={authDeps}>
      <p>Готово</p>
    </AuthProvider>,
    host,
  );
  return { host, authDeps };
}

beforeEach(() => {
  resetSessionState();
});

describe("AuthProvider", () => {
  it("shows the sign-in screen when the browser has no session", async () => {
    const { authDeps } = renderAuth(
      makeFakeHost(),
      deps({ loadMe: vi.fn(async () => ({ kind: "unauthorized" as const })) }),
    );

    expect(await screen.findByText("Нужно войти")).toBeInTheDocument();
    const link = screen.getByRole("link", { name: "Войти" });
    expect(link).toHaveAttribute("href", "/auth/login?return_to=/app/");
    expect(authDeps.loadMe).toHaveBeenCalledTimes(1);
  });

  it("renders the app after a successful browser session", async () => {
    renderAuth();
    expect(await screen.findByText("Готово")).toBeInTheDocument();
  });

  it("shows the binding screen for an unlinked Telegram account", async () => {
    const host = makeFakeHost({ kind: "telegram", isTelegram: true, initData: "hash=1" });
    const { authDeps } = renderAuth(
      host,
      deps({
        signInTelegram: vi.fn(async () => ({ status: "link_required" as const, link_url: "/auth/login?state=1" })),
      }),
    );

    expect(await screen.findByText("Нужно привязать аккаунт")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Привязать аккаунт" }));
    expect(host.openLink).toHaveBeenCalledWith("/auth/login?state=1");
    expect(authDeps.loadMe).not.toHaveBeenCalled();
  });

  it("renders the app after a successful Telegram sign-in", async () => {
    const host = makeFakeHost({ kind: "telegram", isTelegram: true, initData: "hash=1" });
    const { authDeps } = renderAuth(host);

    expect(await screen.findByText("Готово")).toBeInTheDocument();
    expect(authDeps.signInTelegram).toHaveBeenCalledWith("hash=1");
    expect(authDeps.loadMe).toHaveBeenCalledTimes(1);
  });

  it("explains a backend failure", async () => {
    renderAuth(
      makeFakeHost(),
      deps({ loadMe: vi.fn(async () => ({ kind: "error" as const, message: "База недоступна" })) }),
    );

    expect(await screen.findByText("База недоступна")).toBeInTheDocument();
  });
});
