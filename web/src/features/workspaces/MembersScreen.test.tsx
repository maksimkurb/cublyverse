import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { makeCandidate, makeMember, makeWorkspace } from "../../test/fixtures";
import { renderWithHost } from "../../test/render";
import MembersScreen from "./MembersScreen";
import {
  useAddWorkspaceMember,
  useCreateWorkspace,
  useDeleteWorkspace,
  useRemoveWorkspaceMember,
  useRenameWorkspace,
  useWorkspaceCandidates,
  useWorkspaceMembers,
} from "./data";
import { useCurrentWorkspace, writeStoredWorkspaceID } from "./selection";

vi.mock("../../auth/AuthProvider", () => ({
  useAuth: () => ({ user: { id: "99999999-9999-4999-8999-999999999999" }, host: {}, updateUser: vi.fn(), signOut: vi.fn() }),
}));

vi.mock("./data", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./data")>();
  return {
    ...actual,
    useWorkspaceMembers: vi.fn(),
    useWorkspaceCandidates: vi.fn(),
    useCreateWorkspace: vi.fn(),
    useRenameWorkspace: vi.fn(),
    useDeleteWorkspace: vi.fn(),
    useAddWorkspaceMember: vi.fn(),
    useRemoveWorkspaceMember: vi.fn(),
  };
});

vi.mock("./selection", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./selection")>();
  return { ...actual, useCurrentWorkspace: vi.fn(), writeStoredWorkspaceID: vi.fn() };
});

const currentMock = vi.mocked(useCurrentWorkspace);
const membersMock = vi.mocked(useWorkspaceMembers);
const candidatesMock = vi.mocked(useWorkspaceCandidates);
const createMock = vi.mocked(useCreateWorkspace);
const renameMock = vi.mocked(useRenameWorkspace);
const deleteMock = vi.mocked(useDeleteWorkspace);
const addMock = vi.mocked(useAddWorkspaceMember);
const removeMock = vi.mocked(useRemoveWorkspaceMember);
const writeStorageMock = vi.mocked(writeStoredWorkspaceID);

function queryResult(overrides: Record<string, unknown>) {
  return {
    data: undefined,
    isPending: false,
    isError: false,
    isSuccess: true,
    error: null,
    refetch: vi.fn(),
    ...overrides,
  } as never;
}

type MutationStub = { mutateAsync: ReturnType<typeof vi.fn> };

function mutation(result: unknown = undefined): MutationStub {
  return { mutateAsync: vi.fn().mockResolvedValue(result) };
}

beforeEach(() => {
  vi.clearAllMocks();
  const workspace = makeWorkspace();
  currentMock.mockReturnValue({
    query: queryResult({ data: [workspace] }),
    workspaces: [workspace],
    current: workspace,
    select: vi.fn(),
  });
  membersMock.mockReturnValue(queryResult({ data: [makeMember()] }));
  candidatesMock.mockReturnValue(queryResult({ data: [makeCandidate()] }));
  createMock.mockReturnValue(mutation(makeWorkspace({ id: "new", name: "Мастерская" })) as never);
  renameMock.mockReturnValue(mutation(makeWorkspace()) as never);
  deleteMock.mockReturnValue(mutation() as never);
  addMock.mockReturnValue(mutation() as never);
  removeMock.mockReturnValue(mutation() as never);
  writeStorageMock.mockReset();
});

describe("MembersScreen", () => {
  it("lists members with avatars and adds a candidate", async () => {
    const add = mutation();
    addMock.mockReturnValue(add as never);
    renderWithHost(<MembersScreen />);

    expect(screen.getByText("Иван Петров")).toBeInTheDocument();
    expect(screen.getByText("ivan@example.com")).toBeInTheDocument();
    const avatars = document.querySelectorAll("img.avatar");
    expect(avatars.length).toBe(2);
    expect(avatars[0].getAttribute("src")).toContain("gravatar.com");

    expect(screen.getByText("Анна Смирнова")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Добавить: Анна Смирнова" }));
    await waitFor(() => expect(add.mutateAsync).toHaveBeenCalledTimes(1));
    expect(add.mutateAsync.mock.calls[0][0]).toMatchObject({
      params: { path: { id: makeWorkspace().id } },
      body: { user_id: makeCandidate().user_id },
    });
  });

  it("removes a member in two taps", async () => {
    const remove = mutation();
    removeMock.mockReturnValue(remove as never);
    renderWithHost(<MembersScreen />);

    const list = screen.getByRole("list", { name: "Участники" });
    fireEvent.click(within(list).getByRole("button", { name: "Удалить" }));
    expect(remove.mutateAsync).not.toHaveBeenCalled();
    fireEvent.click(within(list).getByRole("button", { name: "Подтвердить" }));

    await waitFor(() => expect(remove.mutateAsync).toHaveBeenCalledTimes(1));
    expect(remove.mutateAsync.mock.calls[0][0]).toMatchObject({
      params: { path: { id: makeWorkspace().id, user_id: makeMember().user_id } },
    });
  });

  it("creates a workspace and selects it", async () => {
    const create = mutation(makeWorkspace({ id: "new", name: "Мастерская" }));
    createMock.mockReturnValue(create as never);
    const select = vi.fn();
    currentMock.mockReturnValue({
      query: queryResult({ data: [] }),
      workspaces: [],
      current: null,
      select,
    });
    renderWithHost(<MembersScreen />);

    expect(screen.getByText("Нет пространств")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Новое пространство" }));
    fireEvent.change(screen.getByLabelText("Название"), { target: { value: "Мастерская" } });
    fireEvent.click(screen.getByRole("button", { name: "Создать пространство" }));

    await waitFor(() => expect(create.mutateAsync).toHaveBeenCalledTimes(1));
    expect(create.mutateAsync.mock.calls[0][0]).toMatchObject({ body: { name: "Мастерская" } });
    await waitFor(() => expect(select).toHaveBeenCalledWith("new"));
  });

  it("renames and deletes a workspace with a confirming tap", async () => {
    const rename = mutation(makeWorkspace({ name: "Мастерская" }));
    renameMock.mockReturnValue(rename as never);
    const remove = mutation();
    deleteMock.mockReturnValue(remove as never);
    renderWithHost(<MembersScreen />);

    fireEvent.click(screen.getByRole("button", { name: "Переименовать" }));
    fireEvent.change(screen.getByLabelText("Название"), { target: { value: "Мастерская" } });
    fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));
    await waitFor(() => expect(rename.mutateAsync).toHaveBeenCalledTimes(1));
    expect(rename.mutateAsync.mock.calls[0][0]).toMatchObject({
      params: { path: { id: makeWorkspace().id } },
      body: { name: "Мастерская" },
    });

    const wsSection = screen.getByRole("region", { name: "Пространство Дом" });
    fireEvent.click(within(wsSection).getByRole("button", { name: "Удалить" }));
    expect(remove.mutateAsync).not.toHaveBeenCalled();
    fireEvent.click(within(wsSection).getByRole("button", { name: "Подтвердить удаление" }));
    await waitFor(() => expect(remove.mutateAsync).toHaveBeenCalledTimes(1));
    expect(writeStorageMock).toHaveBeenCalledWith(null);
  });
});
