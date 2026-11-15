import { vi } from "vitest";

import type { HostBridge, HostTheme } from "../host/types";

/** Host bridge for component tests: no Telegram, spied alerts and haptics. */
export function makeFakeHost(overrides: Partial<HostBridge> = {}): HostBridge {
  const theme: HostTheme = "light";
  return {
    kind: "browser",
    isTelegram: false,
    get theme() {
      return theme;
    },
    initData: "",
    init: vi.fn(),
    onThemeChange: vi.fn((listener: (next: HostTheme) => void) => {
      void listener;
      return () => {};
    }),
    haptic: vi.fn(),
    showAlert: vi.fn(() => Promise.resolve()),
    showConfirm: vi.fn(() => Promise.resolve(true)),
    openLink: vi.fn(),
    close: vi.fn(),
    backButton: {
      show: vi.fn(),
      hide: vi.fn(),
      onClick: vi.fn(() => () => {}),
    },
    ...overrides,
  };
}
