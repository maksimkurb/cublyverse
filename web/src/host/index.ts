import { createBrowserBridge, createTelegramBridge, isTelegramHost, telegramWebApp, type HostWindow } from "./bridges";
import type { HostBridge, HostKind } from "./types";

/** Window fields used for host detection. */
export interface DetectWindow extends HostWindow {
  parent?: unknown;
  location?: {
    href: string;
    hash?: string;
    search?: string;
    origin?: string;
    assign?: (url: string) => void;
  };
}

/**
 * Matrix widget context travels in the URL fragment (as Element recommends)
 * with query-string compatibility for older widgets. The widget API path is a
 * documented stub: the WebApp detects it and then behaves like a browser.
 */
export function widgetParams(location: { hash?: string; search?: string }): URLSearchParams {
  const hash = (location.hash ?? "").replace(/^#\/?\??/, "");
  return hash ? new URLSearchParams(hash) : new URLSearchParams(location.search ?? "");
}

/** Detects the host of the current page. */
export function detectHostKind(win: DetectWindow): HostKind {
  if (isTelegramHost(win)) {
    return "telegram";
  }
  const embedded = Boolean(win.parent) && win.parent !== win;
  if (embedded && widgetParams(win.location ?? {}).has("widgetId")) {
    return "matrix";
  }
  return "browser";
}

/**
 * Builds the host bridge for a window. Telegram gets the real SDK bridge;
 * Matrix and plain browsers share the browser bridge.
 */
export function createHostBridge(win: DetectWindow): HostBridge {
  const kind = detectHostKind(win);
  if (kind === "telegram") {
    const webApp = telegramWebApp(win);
    if (webApp) {
      return createTelegramBridge(win, webApp);
    }
  }
  return createBrowserBridge(win, kind === "matrix" ? "matrix" : "browser");
}

/** Bridge of the current page. */
export const host: HostBridge = createHostBridge(window as DetectWindow);
