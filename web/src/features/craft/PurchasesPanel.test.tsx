import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { makeItem, makeWish, makeWorkspace } from "../../test/fixtures";
import { renderWithHost } from "../../test/render";
import PurchasesPanel from "./PurchasesPanel";
import {
  useAddWishLink,
  useCraftCatalog,
  useCreateWish,
  useDeleteWish,
  useMarkWishBought,
  useRemoveWishLink,
  useUpdateWish,
  useWishes,
} from "./data";

vi.mock("./data", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./data")>();
  return {
    ...actual,
    useWishes: vi.fn(),
    useCraftCatalog: vi.fn(),
    useCreateWish: vi.fn(),
    useUpdateWish: vi.fn(),
    useDeleteWish: vi.fn(),
    useAddWishLink: vi.fn(),
    useRemoveWishLink: vi.fn(),
    useMarkWishBought: vi.fn(),
  };
});

const wishesMock = vi.mocked(useWishes);
const catalogMock = vi.mocked(useCraftCatalog);
const createMock = vi.mocked(useCreateWish);
const updateMock = vi.mocked(useUpdateWish);
const deleteMock = vi.mocked(useDeleteWish);
const addLinkMock = vi.mocked(useAddWishLink);
const removeLinkMock = vi.mocked(useRemoveWishLink);
const boughtMock = vi.mocked(useMarkWishBought);

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
  wishesMock.mockReturnValue(queryResult({ data: [] }));
  catalogMock.mockReturnValue(queryResult({ data: [] }));
  createMock.mockReturnValue(mutation() as never);
  updateMock.mockReturnValue(mutation() as never);
  deleteMock.mockReturnValue(mutation() as never);
  addLinkMock.mockReturnValue(mutation() as never);
  removeLinkMock.mockReturnValue(mutation() as never);
  boughtMock.mockReturnValue(mutation(makeWish({ status: "bought" })) as never);
});

describe("PurchasesPanel", () => {
  it("groups entries by status and opens shop links in a new tab", () => {
    wishesMock.mockReturnValue(
      queryResult({
        data: [
          makeWish(),
          makeWish({
            id: "22222222-2222-4222-8222-222222222222",
            title: "Лента атласная",
            catalog_item_id: "",
            item: undefined,
            qty_wanted: null,
            priority: 0,
            note: "",
            status: "ordered",
            links: [],
          }),
        ],
      }),
    );

    renderWithHost(<PurchasesPanel workspaceId={makeWorkspace().id} />);

    expect(screen.getByRole("heading", { name: /Хочу купить/ })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: /Заказано/ })).toBeInTheDocument();
    expect(screen.getByText("Лента атласная")).toBeInTheDocument();
    expect(screen.getByText("Очень важно")).toBeInTheDocument();
    expect(screen.getByText(/3 шт/)).toBeInTheDocument();

    const link = screen.getByRole("link", { name: /Магазин бусин/ });
    expect(link).toHaveAttribute("href", "https://shop.example/beads");
    expect(link).toHaveAttribute("target", "_blank");
    expect(link.getAttribute("rel")).toContain("noopener");
    expect(link.getAttribute("rel")).toContain("noreferrer");
    expect(screen.getByText("12,5 RUB")).toBeInTheDocument();
    expect(screen.getByText("в наличии")).toBeInTheDocument();
  });

  it("edits the quantity, note and status of an entry", async () => {
    const update = mutation(makeWish({ status: "ordered" }));
    updateMock.mockReturnValue(update as never);
    wishesMock.mockReturnValue(queryResult({ data: [makeWish()] }));

    renderWithHost(<PurchasesPanel workspaceId={makeWorkspace().id} />);

    fireEvent.click(screen.getByRole("button", { name: "Изменить: Жемчуг айвори 2 мм" }));
    fireEvent.change(screen.getByLabelText("Сколько нужно"), { target: { value: "5" } });
    fireEvent.change(screen.getByLabelText("Статус"), { target: { value: "ordered" } });
    fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));

    await waitFor(() => expect(update.mutateAsync).toHaveBeenCalledTimes(1));
    expect(update.mutateAsync.mock.calls[0][0]).toMatchObject({
      params: { path: { id: makeWorkspace().id, wish_id: makeWish().id } },
      body: { qty_wanted: 5, priority: 2, note: "на подарок", status: "ordered" },
    });
  });

  it("marks an entry bought with the chosen stock behaviour", async () => {
    const bought = mutation(makeWish({ status: "bought" }));
    boughtMock.mockReturnValue(bought as never);
    wishesMock.mockReturnValue(queryResult({ data: [makeWish()] }));

    renderWithHost(<PurchasesPanel workspaceId={makeWorkspace().id} />);

    fireEvent.click(screen.getByRole("button", { name: "Куплено: Жемчуг айвори 2 мм" }));
    expect(screen.getByText(/перейдёт в статус «Куплено»/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(screen.getByRole("button", { name: "Отметить купленным" }));

    await waitFor(() => expect(bought.mutateAsync).toHaveBeenCalledTimes(1));
    expect(bought.mutateAsync.mock.calls[0][0]).toMatchObject({
      params: { path: { id: makeWorkspace().id, wish_id: makeWish().id } },
      body: { add_to_stock: false },
    });
  });

  it("adds a catalogue item with a quantity and a priority", async () => {
    const create = mutation(makeWish());
    createMock.mockReturnValue(create as never);
    catalogMock.mockReturnValue(queryResult({ data: [makeItem()] }));

    renderWithHost(<PurchasesPanel workspaceId={makeWorkspace().id} />);

    fireEvent.click(screen.getByRole("button", { name: "Добавить в закупки" }));
    fireEvent.click(await screen.findByRole("button", { name: "Выбрать: Жемчуг айвори 2 мм" }));
    fireEvent.change(screen.getByLabelText("Сколько нужно"), { target: { value: "2" } });
    fireEvent.change(screen.getByLabelText("Приоритет"), { target: { value: "1" } });
    fireEvent.click(screen.getByRole("button", { name: "Добавить" }));

    await waitFor(() => expect(create.mutateAsync).toHaveBeenCalledTimes(1));
    expect(create.mutateAsync.mock.calls[0][0]).toMatchObject({
      params: { path: { id: makeWorkspace().id } },
      body: { catalog_item_id: makeItem().id, qty_wanted: 2, priority: 1, note: "" },
    });
  });
});
