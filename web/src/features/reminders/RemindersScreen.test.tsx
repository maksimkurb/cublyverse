import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Me } from "../../api/client";
import { makeFakeHost } from "../../test/fakeHost";
import { makeReminder } from "../../test/fixtures";
import { renderWithHost } from "../../test/render";
import RemindersScreen from "./RemindersScreen";
import { useCompleteReminder, useRemindersQuery, useUpdateReminder } from "./data";

vi.mock("./data", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./data")>();
  return {
    ...actual,
    useRemindersQuery: vi.fn(),
    useUpdateReminder: vi.fn(),
    useCompleteReminder: vi.fn(),
  };
});

vi.mock("../workspaces/selection", () => ({
  useCurrentWorkspace: () => ({ current: { id: "44444444-4444-4444-8444-444444444444", name: "Личное" } }),
}));
vi.mock("../workspaces/SpaceBar", () => ({ SpaceButton: () => null, SpaceState: () => null }));
vi.mock("../workspaces/data", () => ({ useWorkspaceMembers: () => ({ data: [] }) }));

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
const completeMock = vi.mocked(useCompleteReminder);

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
  completeMock.mockReset();
  completeMock.mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue(undefined),
  } as unknown as ReturnType<typeof useCompleteReminder>);
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
    expect(screen.getByText("каждую неделю по пн, ср, пт в 09:30 · повтор каждые 10 мин")).toBeInTheDocument();
    expect(screen.getByText("Следующее: 12.01 09:30")).toBeInTheDocument();
    expect(screen.getByRole("switch")).toBeChecked();
    expect(screen.getByRole("button", { name: "Новое напоминание" })).toBeInTheDocument();
  });

  it("confirms a pending occurrence with the Выполнено button", async () => {
    queryMock.mockReturnValue(
      queryResult({
        data: [
          makeReminder({
            active_occurrence: {
              id: "33333333-3333-4333-8333-333333333333",
              reminder_id: "11111111-1111-4111-8111-111111111111",
              scheduled_at: "2026-01-12T06:30:00Z",
              first_sent_at: "2026-01-12T06:30:00Z",
              repeat_count: 1,
              next_repeat_at: null,
              status: "active",
              completed_at: null,
            },
          }),
        ],
      }),
    );
    const mutateAsync = vi.fn().mockResolvedValue(undefined);
    completeMock.mockReturnValue({ mutateAsync } as unknown as ReturnType<typeof useCompleteReminder>);
    renderScreen();

    expect(screen.getByText("Ждёт отметки")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Выполнено" }));

    await waitFor(() =>
      expect(mutateAsync).toHaveBeenCalledWith({
        params: { path: { id: "11111111-1111-4111-8111-111111111111" } },
      }),
    );
  });

  it("filters the list by one-time reminders", () => {
    queryMock.mockReturnValue(
      queryResult({
        data: [
          makeReminder(),
          makeReminder({
            id: "44444444-4444-4444-8444-444444444444",
            message: "Записаться к врачу",
            schedule_kind: "once",
            schedule_days: [],
            start_date: "2027-03-20",
          }),
        ],
      }),
    );
    renderScreen();

    fireEvent.click(screen.getByRole("button", { name: "Один раз" }));

    expect(screen.queryByText("Выпить таблетку")).not.toBeInTheDocument();
    expect(screen.getByText("Записаться к врачу")).toBeInTheDocument();
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
