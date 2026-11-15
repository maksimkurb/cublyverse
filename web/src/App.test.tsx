import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import App from "./App";
import { fetchHealth } from "./api/client";

vi.mock("./api/client", () => ({
  fetchHealth: vi.fn(),
}));

const mockedFetchHealth = vi.mocked(fetchHealth);

describe("App", () => {
  it("shows the version reported by the backend", async () => {
    mockedFetchHealth.mockResolvedValue({ status: "ok", version: "9.9.9" });

    render(<App />);

    expect(await screen.findByTestId("version")).toHaveTextContent("9.9.9");
    expect(screen.getByRole("status")).toHaveTextContent("Бэкенд на связи: ok");
  });

  it("explains an unreachable backend", async () => {
    mockedFetchHealth.mockRejectedValue(new Error("Failed to fetch"));

    render(<App />);

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Бэкенд недоступен: Failed to fetch",
    );
  });
});
