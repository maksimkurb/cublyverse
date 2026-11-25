import { beforeEach, describe, expect, it, vi } from "vitest";

import { WORKSPACE_STORAGE_KEY, readStoredWorkspaceID, writeStoredWorkspaceID } from "./selection";

/** Minimal in-memory localStorage; jsdom does not always provide one. */
function fakeStorage() {
  const values = new Map<string, string>();
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => {
      values.set(key, value);
    },
    removeItem: (key: string) => {
      values.delete(key);
    },
    clear: () => values.clear(),
  };
}

function installStorage(storage: unknown) {
  Object.defineProperty(window, "localStorage", { value: storage, configurable: true });
}

beforeEach(() => {
  installStorage(fakeStorage());
});

describe("workspace selection storage", () => {
  it("remembers and clears the chosen workspace", () => {
    writeStoredWorkspaceID("44444444-4444-4444-8444-444444444444");
    expect(window.localStorage.getItem(WORKSPACE_STORAGE_KEY)).toBe("44444444-4444-4444-8444-444444444444");
    expect(readStoredWorkspaceID()).toBe("44444444-4444-4444-8444-444444444444");

    writeStoredWorkspaceID(null);
    expect(readStoredWorkspaceID()).toBeNull();
  });

  it("keeps working when the browser refuses storage", () => {
    installStorage({
      getItem: vi.fn(() => {
        throw new Error("storage disabled");
      }),
      setItem: vi.fn(() => {
        throw new Error("storage disabled");
      }),
      removeItem: vi.fn(() => {
        throw new Error("storage disabled");
      }),
    });

    expect(() => writeStoredWorkspaceID("id")).not.toThrow();
    expect(readStoredWorkspaceID()).toBeNull();
  });
});
