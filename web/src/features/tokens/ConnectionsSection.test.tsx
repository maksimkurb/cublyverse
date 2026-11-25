import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { makeFakeHost } from "../../test/fakeHost";
import { makeConnection } from "../../test/fixtures";
import { renderWithHost } from "../../test/render";
import { ConnectionsSection } from "./ConnectionsSection";
import { useConnectionsQuery, useRevokeConnection } from "./connections";

vi.mock("./connections", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./connections")>();
  return { ...actual, useConnectionsQuery: vi.fn(), useRevokeConnection: vi.fn() };
});

const queryMock = vi.mocked(useConnectionsQuery);
const revokeMock = vi.mocked(useRevokeConnection);

function queryResult(overrides: Record<string, unknown>) {
  return {
    data: undefined,
    isPending: false,
    isError: false,
    isSuccess: true,
    error: null,
    refetch: vi.fn(),
    ...overrides,
  } as unknown as ReturnType<typeof useConnectionsQuery>;
}

beforeEach(() => {
  queryMock.mockReset();
  revokeMock.mockReset();
});

describe("ConnectionsSection", () => {
  it("renders nothing while there are no connections", () => {
    revokeMock.mockReturnValue({ mutateAsync: vi.fn() } as unknown as ReturnType<typeof useRevokeConnection>);
    queryMock.mockReturnValue(queryResult({ data: [] }));
    renderWithHost(<ConnectionsSection />, makeFakeHost());
    expect(screen.queryByText("Подключённые приложения")).not.toBeInTheDocument();
  });

  it("lists connections and disconnects with a second tap", async () => {
    const mutateAsync = vi.fn().mockResolvedValue(undefined);
    revokeMock.mockReturnValue({ mutateAsync } as unknown as ReturnType<typeof useRevokeConnection>);
    queryMock.mockReturnValue(queryResult({ data: [makeConnection()] }));
    renderWithHost(<ConnectionsSection />, makeFakeHost());

    expect(screen.getByText("Claude")).toBeInTheDocument();
    expect(screen.getByText("Дом")).toBeInTheDocument();
    expect(screen.getByText("craft:write")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Отключить" }));
    expect(mutateAsync).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Подтвердить отключение" }));
    await waitFor(() =>
      expect(mutateAsync).toHaveBeenCalledWith({
        params: { path: { id: "33333333-3333-4333-8333-333333333333" } },
      }),
    );
  });
});
