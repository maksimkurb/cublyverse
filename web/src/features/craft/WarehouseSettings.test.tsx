import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { makeFakeHost } from "../../test/fakeHost";
import { makeCategory, makeWorkspace } from "../../test/fixtures";
import { renderWithHost } from "../../test/render";
import { useCraftReference, useReferenceMutations } from "./data";
import { WarehouseSettings } from "./WarehouseSettings";

vi.mock("./data", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./data")>();
  return { ...actual, useCraftReference: vi.fn(), useReferenceMutations: vi.fn() };
});

const names = [
  "createCategory",
  "updateCategory",
  "deleteCategory",
  "createManufacturer",
  "updateManufacturer",
  "deleteManufacturer",
  "createUnit",
  "updateUnit",
  "deleteUnit",
  "setCatalogs",
] as const;

let mutations: Record<(typeof names)[number], { mutateAsync: ReturnType<typeof vi.fn> }>;

beforeEach(() => {
  mutations = Object.fromEntries(names.map((name) => [name, { mutateAsync: vi.fn().mockResolvedValue({}) }])) as typeof mutations;
  vi.mocked(useReferenceMutations).mockReturnValue(mutations as never);
  vi.mocked(useCraftReference).mockReturnValue({
    data: {
      categories: [makeCategory()],
      manufacturers: [
        { id: "m1", name: "DMC", website: "", official: true, own_items: 0 },
        { id: "m2", name: "Gamma", website: "", official: false, own_items: 1 },
      ],
      units: [{ id: "u1", name: "шт" }],
      catalogs: [{ source: "dmc", name: "DMC (мулине)", enabled: false, items: 508 }],
    },
    isPending: false,
    isError: false,
  } as never);
});

const workspaceId = makeWorkspace().id;

function open(host = makeFakeHost()) {
  renderWithHost(<WarehouseSettings workspaceId={workspaceId} />, host);
  fireEvent.click(screen.getByRole("button", { name: "Настройки склада" }));
}

describe("WarehouseSettings", () => {
  it("creates a category with a list field", async () => {
    open();
    fireEvent.click(screen.getByRole("button", { name: "Новая категория" }));
    fireEvent.change(screen.getByLabelText("Название категории"), { target: { value: "Лента" } });
    fireEvent.click(screen.getByRole("button", { name: "Добавить поле" }));
    fireEvent.change(screen.getByLabelText("Название поля"), { target: { value: "Фактура" } });
    fireEvent.change(screen.getByLabelText("Тип"), { target: { value: "select" } });
    fireEvent.change(screen.getByLabelText("Варианты через запятую"), { target: { value: "атлас, репс, " } });
    fireEvent.click(screen.getByRole("button", { name: "Сохранить категорию" }));

    await waitFor(() => expect(mutations.createCategory.mutateAsync).toHaveBeenCalledTimes(1));
    expect(mutations.createCategory.mutateAsync.mock.calls[0][0]).toMatchObject({
      params: { path: { id: workspaceId } },
      body: { name: "Лента", default_unit: "шт", fields: [{ label: "Фактура", type: "select", options: ["атлас", "репс"] }] },
    });
  });

  it("keeps official manufacturers read-only and connects a catalogue", async () => {
    open();
    fireEvent.click(screen.getByText("Бренды"));
    expect(screen.queryByRole("button", { name: "Изменить: DMC" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Изменить: Gamma" })).toBeInTheDocument();

    fireEvent.click(screen.getByText("Каталоги"));
    fireEvent.click(screen.getByRole("checkbox"));
    await waitFor(() => expect(mutations.setCatalogs.mutateAsync).toHaveBeenCalledTimes(1));
    expect(mutations.setCatalogs.mutateAsync.mock.calls[0][0]).toMatchObject({ body: { sources: ["dmc"] } });
  });
});
