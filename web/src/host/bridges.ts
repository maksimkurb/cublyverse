import type { TelegramThemeParams, TelegramWebApp } from "./telegramTypes";
import type { BackButton, HapticKind, HostBridge, HostTheme } from "./types";

/** Media query for the browser colour scheme. */
const darkQuery = "(prefers-color-scheme: dark)";

/** The browser window surface the bridge touches; tests pass a fake. */
export interface HostWindow {
  Telegram?: { WebApp?: TelegramWebApp };
  matchMedia?: (query: string) => {
    matches: boolean;
    addEventListener?: (type: string, listener: () => void) => void;
    removeEventListener?: (type: string, listener: () => void) => void;
  };
  alert?: (message: string) => void;
  confirm?: (message: string) => boolean;
  open?: (url: string, target?: string) => unknown;
  close?: () => void;
  location?: { href: string; assign?: (url: string) => void; origin?: string };
  parent?: unknown;
  document?: {
    documentElement: {
      style: { setProperty(name: string, value: string): void };
      dataset: Record<string, string | undefined>;
    };
  };
}

/** Returns `Telegram.WebApp` when the Mini App script bootstraped. */
export function telegramWebApp(win: HostWindow): TelegramWebApp | undefined {
  return win.Telegram?.WebApp;
}

/** True when the page runs inside the Telegram Mini App. */
export function isTelegramHost(win: HostWindow): boolean {
  return Boolean(telegramWebApp(win)?.initData);
}

/** Browser colour scheme, used outside Telegram. */
export function browserTheme(win: HostWindow): HostTheme {
  return win.matchMedia?.(darkQuery).matches ? "dark" : "light";
}

/**
 * Copies Telegram theme parameters to CSS custom properties on `<html>`.
 *
 * telegram-ui reads the same `--tg-theme-*` variables, so its components follow
 * the Telegram client theme without any extra wiring.
 */
export function applyTelegramTheme(win: HostWindow, webApp: TelegramWebApp): HostTheme {
  const theme: HostTheme = webApp.colorScheme === "dark" ? "dark" : "light";
  const root = win.document?.documentElement;
  if (root) {
    root.dataset.theme = theme;
    for (const [key, value] of Object.entries(webApp.themeParams as TelegramThemeParams)) {
      if (typeof value === "string" && value !== "") {
        root.style.setProperty(`--tg-theme-${key.replaceAll("_", "-")}`, value);
      }
    }
  }
  return theme;
}

function setDocumentTheme(win: HostWindow, theme: HostTheme) {
  const root = win.document?.documentElement;
  if (root) {
    root.dataset.theme = theme;
  }
}

function isSameOrigin(win: HostWindow, url: string): boolean {
  if (url.startsWith("/") && !url.startsWith("//")) {
    return true;
  }
  try {
    const base = win.location?.origin ?? "http://localhost";
    return new URL(url, base).origin === base;
  } catch {
    return false;
  }
}

function noopBackButton(): BackButton {
  return {
    show: () => {},
    hide: () => {},
    onClick: () => () => {},
  };
}

/** Host bridge for a plain browser (and the Matrix widget fallback). */
export function createBrowserBridge(win: HostWindow, kind: "browser" | "matrix" = "browser"): HostBridge {
  let theme = browserTheme(win);
  const listeners = new Set<(theme: HostTheme) => void>();

  const media = win.matchMedia?.(darkQuery);
  media?.addEventListener?.("change", () => {
    theme = media.matches ? "dark" : "light";
    setDocumentTheme(win, theme);
    for (const listener of listeners) {
      listener(theme);
    }
  });

  return {
    kind,
    isTelegram: false,
    get theme() {
      return theme;
    },
    initData: "",
    init() {
      setDocumentTheme(win, theme);
      win.document?.documentElement.style.setProperty("color-scheme", theme);
    },
    onThemeChange(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    haptic() {
      // No haptics outside a messenger.
    },
    showAlert(message) {
      win.alert?.(message);
      return Promise.resolve();
    },
    showConfirm(message) {
      return Promise.resolve(win.confirm?.(message) ?? false);
    },
    openLink(url) {
      if (isSameOrigin(win, url)) {
        win.location?.assign?.(url);
        return;
      }
      const opened = win.open?.(url, "_blank");
      if (!opened && win.location?.assign) {
        win.location.assign(url);
      }
    },
    close() {
      win.close?.();
    },
    backButton: noopBackButton(),
  };
}

/** Host bridge backed by the Telegram Mini App SDK. */
export function createTelegramBridge(win: HostWindow, webApp: TelegramWebApp): HostBridge {
  let theme: HostTheme = webApp.colorScheme === "dark" ? "dark" : "light";
  const listeners = new Set<(theme: HostTheme) => void>();
  webApp.onEvent("themeChanged", () => {
    theme = applyTelegramTheme(win, webApp);
    for (const listener of listeners) {
      listener(theme);
    }
  });

  return {
    kind: "telegram",
    isTelegram: true,
    get theme() {
      return theme;
    },
    initData: webApp.initData,
    init() {
      theme = applyTelegramTheme(win, webApp);
      webApp.ready();
      webApp.expand();
    },
    onThemeChange(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    haptic(kind: HapticKind) {
      const feedback = webApp.HapticFeedback;
      if (!feedback) {
        return;
      }
      switch (kind) {
        case "selection":
          feedback.selectionChanged();
          break;
        case "success":
          feedback.notificationOccurred("success");
          break;
        case "warning":
          feedback.notificationOccurred("warning");
          break;
        case "error":
          feedback.notificationOccurred("error");
          break;
      }
    },
    showAlert(message) {
      return new Promise<void>((resolve) => {
        webApp.showAlert(message, () => resolve());
      });
    },
    showConfirm(message) {
      return new Promise<boolean>((resolve) => {
        webApp.showConfirm(message, (confirmed) => resolve(confirmed));
      });
    },
    openLink(url) {
      if (isSameOrigin(win, url)) {
        // A binding link must stay in the same webview: the next page has to
        // see `initData` of this Telegram account.
        win.location?.assign?.(url);
        return;
      }
      webApp.openLink(url);
    },
    close() {
      webApp.close();
    },
    backButton: {
      show() {
        webApp.BackButton?.show();
      },
      hide() {
        webApp.BackButton?.hide();
      },
      onClick(listener) {
        webApp.BackButton?.onClick(listener);
        return () => webApp.BackButton?.offClick(listener);
      },
    },
  };
}
