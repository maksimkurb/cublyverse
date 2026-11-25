import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { makeCandidate, makeMember, makeWorkspace } from "../../test/fixtures";
import { renderWithHost } from "../../test/render";
import { SpaceBar } from "./SpaceBar";
import {
  useAddWorkspaceMember,
  useCreateWorkspace,
  useDeleteWorkspace,
  useRemoveWorkspaceMember,
  useRenameWorkspace,
  useWorkspaceCandidates,
  useWorkspaceMembers,
} from "./data";
import type { CurrentWorkspace } from "./selection";

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

type MutationStub = { mutateAsync: ReturnType<typeof vi.fn> };

function mutation(result: unknown = undefined): MutationStub {
  return { mutateAsync: vi.fn().mockResolvedValue(result) };
}

function queryResult(data: unknown) {
  return { data, isPending: false, isError: false, isSuccess: true, error: null, refetch: vi.fn() } as never;
}

function spaceState(spaces = [makeWorkspace()]): CurrentWorkspace {
  return { query: queryResult(spaces), workspaces: spaces, current: spaces[0] ?? null, select: vi.fn() };
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(useWorkspaceMembers).mockReturnValue(queryResult([makeMember()]));
  vi.mocked(useWorkspaceCandidates).mockReturnValue(queryResult([makeCandidate()]));
  vi.mocked(useCreateWorkspace).mockReturnValue(mutation() as never);
  vi.mocked(useRenameWorkspace).mockReturnValue(mutation() as never);
  vi.mocked(useDeleteWorkspace).mockReturnValue(mutation() as never);
  vi.mocked(useAddWorkspaceMember).mockReturnValue(mutation() as never);
  vi.mocked(useRemoveWorkspaceMember).mockReturnValue(mutation() as never);
});

function openSheet() {
  fireEvent.click(screen.getByRole("button", { name: /Пространство: Дом/ }));
}

describe("SpaceBar", () => {
  it("onboards a new user with a personal space of the mini-app", async () => {
    const create = mutation(makeWorkspace({ id: "new", app: "reminders" }));
    vi.mocked(useCreateWorkspace).mockReturnValue(create as never);
    const space = spaceState([]);
    renderWithHost(<SpaceBar app="reminders" space={space} />);

    expect(screen.getByLabelText("Название")).toHaveValue("Личное");
    fireEvent.click(screen.getByRole("button", { name: "Начать" }));

    await waitFor(() => expect(space.select).toHaveBeenCalledWith("new"));
    expect(create.mutateAsync.mock.calls[0][0]).toEqual({ body: { app: "reminders", name: "Личное" } });
  });

  it("lists members of the current space and adds a candidate", async () => {
    const add = mutation();
    vi.mocked(useAddWorkspaceMember).mockReturnValue(add as never);
    renderWithHost(<SpaceBar app="warehouse" space={spaceState()} />);
    openSheet();

    expect(screen.getByText("Иван Петров")).toBeInTheDocument();
    expect(document.querySelectorAll("img.avatar").length).toBe(2);
    fireEvent.click(screen.getByRole("button", { name: "Добавить: Анна Смирнова" }));

    await waitFor(() => expect(add.mutateAsync).toHaveBeenCalledTimes(1));
    expect(add.mutateAsync.mock.calls[0][0]).toMatchObject({
      params: { path: { id: makeWorkspace().id } },
      body: { user_id: makeCandidate().user_id },
    });
  });

  it("removes a member and deletes the space with a confirming tap", async () => {
    const removeMember = mutation();
    vi.mocked(useRemoveWorkspaceMember).mockReturnValue(removeMember as never);
    const removeSpace = mutation();
    vi.mocked(useDeleteWorkspace).mockReturnValue(removeSpace as never);
    renderWithHost(<SpaceBar app="warehouse" space={spaceState()} />);
    openSheet();

    const members = screen.getByRole("list", { name: "Участники" });
    fireEvent.click(within(members).getByRole("button", { name: "Удалить" }));
    expect(removeMember.mutateAsync).not.toHaveBeenCalled();
    fireEvent.click(within(members).getByRole("button", { name: "Подтвердить" }));
    await waitFor(() => expect(removeMember.mutateAsync).toHaveBeenCalledTimes(1));

    fireEvent.click(screen.getByRole("button", { name: "Удалить пространство" }));
    expect(removeSpace.mutateAsync).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Подтвердить удаление" }));
    await waitFor(() => expect(removeSpace.mutateAsync).toHaveBeenCalledTimes(1));
  });
});
