import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Me } from "../../api/client";
import { makeFakeHost } from "../../test/fakeHost";
import { makeReminder } from "../../test/fixtures";
import { renderWithHost } from "../../test/render";
import RemindersScreen from "./RemindersScreen";
import { useRemindersQuery, useUpdateReminder } from "./data";

vi.mock("./data", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./data")>();
  return {
    ...actual,
    useRemindersQuery: vi.fn(),
    useUpdateReminder: vi.fn(),
  };
});

const user: Me = {
  id: "user-1",
  email: null,
  display_name: null,
  timezone: "Europe/Moscow",
  created_at: "2026-01-15T09:30:00Z",
  bindings: [],
};

vi.mock("../../auth/AuthProvider", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../auth/AuthProvider")>();
  return {
    ...actual,
    useAuth: () => ({
      user,
      host: makeFakeHost(),
      updateUser: vi.fn(),
      signOut: vi.fn(),
    }),
  };
});

const queryMock = vi.mocked(useRemindersQuery);
const updateMock = vi.mocked(useUpdateReminder);

function queryResult(overrides: Record<string, unknown>) {
  return {
    data: undefined,
    isPending: false,
    isError: false,
    isSuccess: true,
    error: null,
    refetch: vi.fn(),
    ...overrides,
  } as unknown as ReturnType<typeof useRemindersQuery>;
}

function renderScreen(host = makeFakeHost()) {
  renderWithHost(<RemindersScreen />, host);
  return host;
}

beforeEach(() => {
  queryMock.mockReset();
  updateMock.mockReset();
  updateMock.mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue(undefined),
  } as unknown as ReturnType<typeof useUpdateReminder>);
});

describe("RemindersScreen", () => {
  it("shows reminders as alarm clock rows", () => {
    queryMock.mockReturnValue(queryResult({ data: [makeReminder()] }));
    renderScreen();

    expect(screen.getByText("09:30")).toBeInTheDocument();
    expect(screen.getByText("Выпить таблетку")).toBeInTheDocument();
    expect(screen.getByText("Пн, Ср, Пт · повтор каждые 10 мин")).toBeInTheDocument();
    expect(screen.getByText("Следующее: 12.01 09:30")).toBeInTheDocument();
    expect(screen.getByRole("switch")).toBeChecked();
    expect(screen.getByRole("button", { name: "＋ Добавить напоминание" })).toBeInTheDocument();
  });

  it("shows the empty state", () => {
    queryMock.mockReturnValue(queryResult({ data: [] }));
    renderScreen();

    expect(screen.getByText("Нет напоминаний")).toBeInTheDocument();
    expect(
      screen.getByText("Добавьте первое напоминание — оно появится здесь как будильник"),
    ).toBeInTheDocument();
  });

  it("shows the load error and a retry button", () => {
    const refetch = vi.fn();
    queryMock.mockReturnValue(
      queryResult({ isError: true, isSuccess: false, error: { code: "internal", message: "Сервер упал" }, refetch }),
    );
    renderScreen();

    expect(screen.getByRole("alert")).toHaveTextContent("Сервер упал");
    fireEvent.click(screen.getByRole("button", { name: "Повторить" }));
    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it("rolls the switch back and shows an alert when the update fails", async () => {
    queryMock.mockReturnValue(queryResult({ data: [makeReminder({ enabled: true })] }));
    const mutateAsync = vi.fn().mockRejectedValue({ code: "internal", message: "Сервер упал" });
    updateMock.mockReturnValue({ mutateAsync } as unknown as ReturnType<typeof useUpdateReminder>);
    const host = renderScreen();

    const control = screen.getByRole("switch");
    expect(control).toBeChecked();
    fireEvent.click(control);

    // Optimistic: the switch flips before the request settles.
    expect(control).not.toBeChecked();

    await waitFor(() =>
      expect(host.showAlert).toHaveBeenCalledWith("Не удалось изменить напоминание: Сервер упал"),
    );
    expect(control).toBeChecked();
    expect(mutateAsync).toHaveBeenCalledWith(
      expect.objectContaining({ body: expect.objectContaining({ enabled: false }) }),
    );
  });

  it("keeps the new state after a successful toggle", async () => {
    queryMock.mockReturnValue(queryResult({ data: [makeReminder({ enabled: true })] }));
    const host = renderScreen();

    fireEvent.click(screen.getByRole("switch"));

    await waitFor(() => expect(screen.getByRole("switch")).not.toBeChecked());
    expect(host.showAlert).not.toHaveBeenCalled();
  });
});
