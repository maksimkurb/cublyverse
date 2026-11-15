import { describe, expect, it } from "vitest";

import { createHostBridge, detectHostKind, widgetParams, type DetectWindow } from "./index";
import type { TelegramWebApp } from "./telegramTypes";

function windowWith(overrides: Partial<DetectWindow>): DetectWindow {
  return {
    location: { href: "https://example.com/app/", hash: "", search: "", origin: "https://example.com" },
    parent: null,
    ...overrides,
  };
}

function telegramApp(overrides: Partial<TelegramWebApp> = {}): TelegramWebApp {
  return {
    initData: "query_id=1&hash=abc",
    colorScheme: "dark",
    themeParams: { bg_color: "#212121", text_color: "#ffffff" },
    ready: () => {},
    expand: () => {},
    close: () => {},
    openLink: () => {},
    showAlert: (_message, callback) => callback?.(),
    showConfirm: (_message, callback) => callback?.(true),
    onEvent: () => {},
    offEvent: () => {},
    ...overrides,
  };
}

describe("host detection", () => {
  it("detects the Telegram Mini App", () => {
    const win = windowWith({ Telegram: { WebApp: telegramApp() } });
    expect(detectHostKind(win)).toBe("telegram");
    expect(createHostBridge(win).isTelegram).toBe(true);
  });

  it("detects an embedded Matrix widget", () => {
    const win = windowWith({
      parent: {},
      location: {
        href: "https://example.com/app/#?widgetId=w-1&userId=@alice:example.com",
        hash: "#?widgetId=w-1&userId=@alice:example.com",
        search: "",
        origin: "https://example.com",
      },
    });
    expect(detectHostKind(win)).toBe("matrix");
    expect(widgetParams(win.location ?? {}).get("widgetId")).toBe("w-1");
    // The Matrix path is a documented stub: it behaves like a browser.
    expect(createHostBridge(win).isTelegram).toBe(false);
  });

  it("falls back to the browser", () => {
    expect(detectHostKind(windowWith({}))).toBe("browser");
  });
});

describe("Telegram bridge", () => {
  it("applies theme variables and haptics", () => {
    const properties = new Map<string, string>();
    const dataset: Record<string, string | undefined> = {};
    let selectionChanged = 0;
    const webApp = telegramApp({
      HapticFeedback: {
        selectionChanged: () => {
          selectionChanged += 1;
        },
        impactOccurred: () => {},
        notificationOccurred: () => {},
      },
    });
    const win = windowWith({
      Telegram: { WebApp: webApp },
      document: {
        documentElement: {
          style: { setProperty: (name, value) => properties.set(name, value) },
          dataset,
        },
      },
    });

    const host = createHostBridge(win);
    host.init();
    host.haptic("selection");

    expect(host.theme).toBe("dark");
    expect(dataset.theme).toBe("dark");
    expect(properties.get("--tg-theme-bg-color")).toBe("#212121");
    expect(selectionChanged).toBe(1);
  });
});
