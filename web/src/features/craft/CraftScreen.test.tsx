import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { makeCategory, makeItem, makeStock, makeWorkspace } from "../../test/fixtures";
import { renderWithHost } from "../../test/render";
import CraftScreen from "./CraftScreen";
import {
  useAdjustCraftStock,
  useCraftCatalog,
  useCraftCategories,
  useCraftInventory,
  useCraftManufacturers,
  useCreateCraftItem,
  useLinkCraftItem,
  useRemoveCraftStock,
  useSetCraftStock,
  useUpdateCraftStockMeta,
} from "./data";

vi.mock("../workspaces/selection", () => ({
  useCurrentWorkspace: () => ({
    query: { isPending: false, isSuccess: true, isError: false, data: [makeWorkspace()], refetch: vi.fn() },
    workspaces: [makeWorkspace()],
    current: makeWorkspace(),
    select: vi.fn(),
  }),
  writeStoredWorkspaceID: vi.fn(),
}));

vi.mock("./data", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./data")>();
  return {
    ...actual,
    useCraftCategories: vi.fn(),
    useCraftManufacturers: vi.fn(),
    useCraftCatalog: vi.fn(),
    useCraftInventory: vi.fn(),
    useAdjustCraftStock: vi.fn(),
    useCreateCraftItem: vi.fn(),
    useSetCraftStock: vi.fn(),
    useUpdateCraftStockMeta: vi.fn(),
    useRemoveCraftStock: vi.fn(),
    useLinkCraftItem: vi.fn(),
  };
});

const categoriesMock = vi.mocked(useCraftCategories);
const manufacturersMock = vi.mocked(useCraftManufacturers);
const inventoryMock = vi.mocked(useCraftInventory);
const catalogMock = vi.mocked(useCraftCatalog);
const adjustMock = vi.mocked(useAdjustCraftStock);
const createMock = vi.mocked(useCreateCraftItem);
const setStockMock = vi.mocked(useSetCraftStock);
const setMetaMock = vi.mocked(useUpdateCraftStockMeta);
const removeStockMock = vi.mocked(useRemoveCraftStock);
const linkMock = vi.mocked(useLinkCraftItem);

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
  categoriesMock.mockReturnValue(queryResult({ data: [makeCategory()] }));
  manufacturersMock.mockReturnValue(queryResult({ data: [] }));
  inventoryMock.mockReturnValue(queryResult({ data: [] }));
  catalogMock.mockReturnValue(queryResult({ data: [] }));
  adjustMock.mockReturnValue(mutation() as never);
  createMock.mockReturnValue(mutation() as never);
  setStockMock.mockReturnValue(mutation() as never);
  setMetaMock.mockReturnValue(mutation() as never);
  removeStockMock.mockReturnValue(mutation() as never);
  linkMock.mockReturnValue(mutation() as never);
});

describe("CraftScreen", () => {
  it("lists the stock and changes it with the quick buttons and the exact value", async () => {
    const adjust = mutation();
    adjustMock.mockReturnValue(adjust as never);
    const setStock = mutation();
    setStockMock.mockReturnValue(setStock as never);
    const setMeta = mutation();
    setMetaMock.mockReturnValue(setMeta as never);
    inventoryMock.mockReturnValue(queryResult({ data: [makeStock()] }));

    renderWithHost(<CraftScreen />);

    expect(screen.getByRole("heading", { name: "Запасы" })).toBeInTheDocument();
    expect(screen.getByText("Жемчуг айвори 2 мм")).toBeInTheDocument();
    expect(screen.getByText("7 шт")).toBeInTheDocument();
    expect(screen.getByText(/Ящик 2/)).toBeInTheDocument();
    expect(screen.getByText("Куплено на ярмарке")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Добавить 1: Жемчуг айвори 2 мм" }));
    await waitFor(() => expect(adjust.mutateAsync).toHaveBeenCalledTimes(1));
    expect(adjust.mutateAsync.mock.calls[0][0]).toMatchObject({
      params: { path: { id: makeWorkspace().id, item_id: makeItem().id } },
      body: { delta: 1 },
    });

    fireEvent.click(screen.getByRole("switch", { name: /Только малое количество/ }));
    await waitFor(() => {
      const last = inventoryMock.mock.calls[inventoryMock.mock.calls.length - 1][1];
      expect(last.maxQty).toBe(5);
    });

    fireEvent.click(screen.getByRole("button", { name: "Изменить остаток: Жемчуг айвори 2 мм" }));
    fireEvent.change(screen.getByLabelText("Количество, шт"), { target: { value: "12" } });
    fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));

    await waitFor(() => expect(setStock.mutateAsync).toHaveBeenCalledTimes(1));
    expect(setStock.mutateAsync.mock.calls[0][0].body).toMatchObject({ qty: 12 });
    await waitFor(() => expect(setMeta.mutateAsync).toHaveBeenCalledTimes(1));
    expect(setMeta.mutateAsync.mock.calls[0][0].body).toMatchObject({ location: "Ящик 2", notes: "Куплено на ярмарке" });
  });

  it("searches the catalogue, stocks an item and creates a private item", async () => {
    const adjust = mutation();
    adjustMock.mockReturnValue(adjust as never);
    const create = mutation(makeItem());
    createMock.mockReturnValue(create as never);
    catalogMock.mockReturnValue(
      queryResult({
        data: [
          makeItem(),
          makeItem({
            id: "88888888-8888-4888-8888-888888888888",
            private: false,
            workspace_id: null,
            name: "Бисер белый",
            stock_qty: 0,
            in_inventory: false,
          }),
        ],
      }),
    );

    renderWithHost(<CraftScreen />);
    fireEvent.click(screen.getByRole("button", { name: "Каталог" }));

    expect(screen.getByRole("heading", { name: "Каталог" })).toBeInTheDocument();
    expect(screen.getByText("Бисер белый")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Добавить на склад: Бисер белый" }));
    await waitFor(() => expect(adjust.mutateAsync).toHaveBeenCalledTimes(1));
    expect(adjust.mutateAsync.mock.calls[0][0].body).toMatchObject({ delta: 1 });

    fireEvent.click(screen.getByRole("button", { name: "Новая позиция" }));
    fireEvent.change(screen.getByLabelText("Название"), { target: { value: "Жемчуг розовый" } });
    fireEvent.change(screen.getByLabelText("Цвет"), { target: { value: "#ff00aa" } });
    fireEvent.click(screen.getByRole("button", { name: "Создать позицию" }));

    await waitFor(() => expect(create.mutateAsync).toHaveBeenCalledTimes(1));
    expect(create.mutateAsync.mock.calls[0][0]).toMatchObject({
      params: { path: { id: makeWorkspace().id } },
      body: { category: "pearls", name: "Жемчуг розовый", color_hex: "#FF00AA", unit: "шт" },
    });
  });

  it("links a private item to a catalogue item", async () => {
    const link = mutation(makeStock());
    linkMock.mockReturnValue(link as never);
    catalogMock.mockReturnValue(queryResult({ data: [makeItem(), makeItem({ id: "target", private: false, name: "Жемчуг общий" })] }));

    renderWithHost(<CraftScreen />);
    fireEvent.click(screen.getByRole("button", { name: "Каталог" }));
    fireEvent.click(screen.getByRole("button", { name: "Связать с каталогом: Жемчуг айвори 2 мм" }));

    fireEvent.click(await screen.findByRole("button", { name: "Связать: Жемчуг общий" }));
    await waitFor(() => expect(link.mutateAsync).toHaveBeenCalledTimes(1));
    expect(link.mutateAsync.mock.calls[0][0]).toMatchObject({
      params: { path: { id: makeWorkspace().id, item_id: makeItem().id } },
      body: { target_item_id: "target" },
    });
  });
});
