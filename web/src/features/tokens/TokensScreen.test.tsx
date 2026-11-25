import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { CreatedToken } from "../../api/client";
import { makeFakeHost } from "../../test/fakeHost";
import { makeToken } from "../../test/fixtures";
import { renderWithHost } from "../../test/render";
import TokensScreen from "./TokensScreen";
import { useCreateToken, useRevokeToken, useTokensQuery } from "./data";

vi.mock("./ConnectionsSection", () => ({ ConnectionsSection: () => null }));

vi.mock("./data", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./data")>();
  return {
    ...actual,
    useTokensQuery: vi.fn(),
    useCreateToken: vi.fn(),
    useRevokeToken: vi.fn(),
  };
});

const queryMock = vi.mocked(useTokensQuery);
const createMock = vi.mocked(useCreateToken);
const revokeMock = vi.mocked(useRevokeToken);

function queryResult(overrides: Record<string, unknown>) {
  return {
    data: undefined,
    isPending: false,
    isError: false,
    isSuccess: true,
    error: null,
    refetch: vi.fn(),
    ...overrides,
  } as unknown as ReturnType<typeof useTokensQuery>;
}

function renderScreen(host = makeFakeHost()) {
  renderWithHost(<TokensScreen />, host);
  return host;
}

beforeEach(() => {
  queryMock.mockReset();
  createMock.mockReset();
  revokeMock.mockReset();
  revokeMock.mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue(undefined),
  } as unknown as ReturnType<typeof useRevokeToken>);
  createMock.mockReturnValue({
    mutateAsync: vi.fn(),
  } as unknown as ReturnType<typeof useCreateToken>);
});

describe("TokensScreen", () => {
  it("lists tokens with prefix, scopes, state and dates", () => {
    queryMock.mockReturnValue(queryResult({ data: [makeToken()] }));
    renderScreen();

    expect(screen.getByText("Claude Code")).toBeInTheDocument();
    expect(screen.getByText("cv_pat_9xQ2mB7k…")).toBeInTheDocument();
    expect(screen.getByText("profile:read")).toBeInTheDocument();
    expect(screen.getByText("reminders:read")).toBeInTheDocument();
    expect(screen.getByText("Активен")).toBeInTheDocument();
    expect(screen.getByText(/Создан: 15\.01\.2026/)).toBeInTheDocument();
    expect(screen.getByText(/до 15\.04\.2027/)).toBeInTheDocument();
  });

  it("shows revoked and expired tokens without a revoke button", () => {
    queryMock.mockReturnValue(
      queryResult({
        data: [
          makeToken({ id: "revoked", name: "Старый токен", revoked_at: "2026-01-18T00:00:00Z" }),
          makeToken({ id: "expired", name: "Истёкший токен", expires_at: "2026-01-01T00:00:00Z" }),
        ],
      }),
    );
    renderScreen();

    expect(screen.getByText("Отозван")).toBeInTheDocument();
    expect(screen.getByText("Истёк")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Отозвать" })).not.toBeInTheDocument();
  });

  it("shows the empty state", () => {
    queryMock.mockReturnValue(queryResult({ data: [] }));
    renderScreen();

    expect(screen.getByText("Нет токенов")).toBeInTheDocument();
  });

  it("shows the load error and retries", () => {
    const refetch = vi.fn();
    queryMock.mockReturnValue(
      queryResult({ isError: true, isSuccess: false, error: { code: "internal", message: "Сервер упал" }, refetch }),
    );
    renderScreen();

    expect(screen.getByRole("alert")).toHaveTextContent("Сервер упал");
    fireEvent.click(screen.getByRole("button", { name: "Повторить" }));
    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it("revokes in two taps", async () => {
    queryMock.mockReturnValue(queryResult({ data: [makeToken()] }));
    const mutateAsync = vi.fn().mockResolvedValue(undefined);
    revokeMock.mockReturnValue({ mutateAsync } as unknown as ReturnType<typeof useRevokeToken>);
    renderScreen();

    fireEvent.click(screen.getByRole("button", { name: "Отозвать" }));
    expect(mutateAsync).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Подтвердить отзыв" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Подтвердить отзыв" }));
    await waitFor(() => expect(mutateAsync).toHaveBeenCalledTimes(1));
    expect(mutateAsync).toHaveBeenCalledWith({ params: { path: { id: makeToken().id } } });
  });

  it("creates a token and reveals it exactly once", async () => {
    queryMock.mockReturnValue(queryResult({ data: [] }));
    const created: CreatedToken = {
      token: "cv_pat_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG",
      api_token: makeToken({ name: "Новый токен", expires_at: null }),
    };
    const mutateAsync = vi.fn().mockResolvedValue(created);
    createMock.mockReturnValue({ mutateAsync } as unknown as ReturnType<typeof useCreateToken>);
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });

    renderScreen();

    fireEvent.click(screen.getByRole("button", { name: "Новый токен" }));
    fireEvent.change(screen.getByLabelText("Название"), { target: { value: "Новый токен" } });
    fireEvent.click(screen.getByRole("button", { name: "Создать токен" }));

    await waitFor(() => expect(mutateAsync).toHaveBeenCalledTimes(1));
    expect(mutateAsync.mock.calls[0][0].body).toMatchObject({ name: "Новый токен", expires_in_days: null });

    // The reveal dialog carries the plaintext and the "shown once" warning.
    expect(await screen.findByText("Токен создан")).toBeInTheDocument();
    expect(screen.getByLabelText("Значение токена")).toHaveTextContent(created.token);
    expect(screen.getByText(/Больше он не будет показан/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Скопировать" }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(created.token));
    expect(await screen.findByRole("button", { name: "Скопировано" })).toBeInTheDocument();
  });
});
