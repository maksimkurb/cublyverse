import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { makeItem, makeMaterial, makeProject, makeWish, makeWorkspace } from "../../test/fixtures";
import { renderWithHost } from "../../test/render";
import ProjectsPanel from "./ProjectsPanel";
import {
  useCraftCatalog,
  useCreateProject,
  useDeleteProject,
  useProject,
  useProjects,
  useRemoveProjectMaterial,
  useSetProjectMaterial,
  useShortageToWishlist,
  useUpdateProject,
  useUseProjectMaterial,
} from "./data";

vi.mock("./data", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./data")>();
  return {
    ...actual,
    useProjects: vi.fn(),
    useProject: vi.fn(),
    useCreateProject: vi.fn(),
    useUpdateProject: vi.fn(),
    useDeleteProject: vi.fn(),
    useSetProjectMaterial: vi.fn(),
    useRemoveProjectMaterial: vi.fn(),
    useUseProjectMaterial: vi.fn(),
    useShortageToWishlist: vi.fn(),
    useCraftCatalog: vi.fn(),
  };
});

const projectsMock = vi.mocked(useProjects);
const projectMock = vi.mocked(useProject);
const createMock = vi.mocked(useCreateProject);
const updateMock = vi.mocked(useUpdateProject);
const deleteMock = vi.mocked(useDeleteProject);
const setMaterialMock = vi.mocked(useSetProjectMaterial);
const removeMaterialMock = vi.mocked(useRemoveProjectMaterial);
const useMaterialMock = vi.mocked(useUseProjectMaterial);
const shortageMock = vi.mocked(useShortageToWishlist);
const catalogMock = vi.mocked(useCraftCatalog);

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

type MutationStub = { mutateAsync: ReturnType<typeof vi.fn>; isPending?: boolean };

function mutation(result: unknown = undefined): MutationStub {
  return { mutateAsync: vi.fn().mockResolvedValue(result), isPending: false };
}

beforeEach(() => {
  vi.clearAllMocks();
  projectsMock.mockReturnValue(queryResult({ data: [makeProject({ material_count: 1, materials: [] })] }));
  projectMock.mockReturnValue(queryResult({ data: makeProject() }));
  catalogMock.mockReturnValue(queryResult({ data: [] }));
  createMock.mockReturnValue(mutation(makeProject()) as never);
  updateMock.mockReturnValue(mutation(makeProject()) as never);
  deleteMock.mockReturnValue(mutation() as never);
  setMaterialMock.mockReturnValue(mutation(makeProject()) as never);
  removeMaterialMock.mockReturnValue(mutation() as never);
  useMaterialMock.mockReturnValue(mutation(makeProject()) as never);
  shortageMock.mockReturnValue(mutation([makeWish()]) as never);
});

describe("ProjectsPanel", () => {
  it("lists projects and shows the materials of an opened project", () => {
    renderWithHost(<ProjectsPanel workspaceId={makeWorkspace().id} />);

    expect(screen.getByText("Серьги «Майорка»")).toBeInTheDocument();
    expect(screen.getByText("1 материал")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /Серьги «Майорка»/ }));

    expect(screen.getByRole("heading", { name: "Серьги «Майорка»" })).toBeInTheDocument();
    expect(screen.getByText("Подарок на день рождения")).toBeInTheDocument();
    expect(screen.getByText("20 шт")).toBeInTheDocument();
    expect(screen.getByText("5 шт")).toBeInTheDocument();
    expect(screen.getByText("7 шт")).toBeInTheDocument();
    expect(screen.getByText("Нужно купить: 8 шт")).toBeInTheDocument();
  });

  it("withdraws material through the use dialog", async () => {
    const useMaterial = mutation(makeProject());
    useMaterialMock.mockReturnValue(useMaterial as never);

    renderWithHost(<ProjectsPanel workspaceId={makeWorkspace().id} />);
    fireEvent.click(screen.getByRole("button", { name: /Серьги «Майорка»/ }));
    fireEvent.click(screen.getByRole("button", { name: "Использовать: Жемчуг айвори 2 мм" }));

    fireEvent.change(screen.getByLabelText(/Сколько использовать/), { target: { value: "4" } });
    fireEvent.click(screen.getByRole("button", { name: "Использовать" }));

    await waitFor(() => expect(useMaterial.mutateAsync).toHaveBeenCalledTimes(1));
    expect(useMaterial.mutateAsync.mock.calls[0][0]).toMatchObject({
      params: { path: { id: makeWorkspace().id, project_id: makeProject().id, item_id: makeMaterial().item.id } },
      body: { qty: 4 },
    });
  });

  it("adds the project shortage to the wish list", async () => {
    const shortage = mutation([makeWish()]);
    shortageMock.mockReturnValue(shortage as never);

    renderWithHost(<ProjectsPanel workspaceId={makeWorkspace().id} />);
    fireEvent.click(screen.getByRole("button", { name: /Серьги «Майорка»/ }));
    fireEvent.click(screen.getByRole("button", { name: "Недостающее в закупки" }));

    await waitFor(() => expect(shortage.mutateAsync).toHaveBeenCalledTimes(1));
    expect(shortage.mutateAsync.mock.calls[0][0]).toMatchObject({
      params: { path: { id: makeWorkspace().id, project_id: makeProject().id } },
    });
    expect(await screen.findByText("Добавлено в закупки: 1.")).toBeInTheDocument();
  });

  it("plans a catalogue item with the default quantity", async () => {
    const setMaterial = mutation(makeProject());
    setMaterialMock.mockReturnValue(setMaterial as never);
    catalogMock.mockReturnValue(queryResult({ data: [makeItem({ id: "new-item", name: "Бисер белый" })] }));

    renderWithHost(<ProjectsPanel workspaceId={makeWorkspace().id} />);
    fireEvent.click(screen.getByRole("button", { name: /Серьги «Майорка»/ }));
    fireEvent.click(screen.getByRole("button", { name: "Добавить материал" }));

    fireEvent.click(await screen.findByRole("button", { name: "Добавить материал: Бисер белый" }));

    await waitFor(() => expect(setMaterial.mutateAsync).toHaveBeenCalledTimes(1));
    expect(setMaterial.mutateAsync.mock.calls[0][0]).toMatchObject({
      params: { path: { id: makeWorkspace().id, project_id: makeProject().id, item_id: "new-item" } },
      body: { qty_planned: 1 },
    });
  });

  it("creates a project from the header action", async () => {
    const create = mutation(makeProject());
    createMock.mockReturnValue(create as never);

    renderWithHost(<ProjectsPanel workspaceId={makeWorkspace().id} />);
    fireEvent.click(screen.getByRole("button", { name: "Новый проект" }));
    fireEvent.change(screen.getByLabelText("Название"), { target: { value: "Браслет" } });
    fireEvent.click(screen.getByRole("button", { name: "Создать проект" }));

    await waitFor(() => expect(create.mutateAsync).toHaveBeenCalledTimes(1));
    expect(create.mutateAsync.mock.calls[0][0]).toMatchObject({
      params: { path: { id: makeWorkspace().id } },
      body: { name: "Браслет", notes: "" },
    });
  });
});
