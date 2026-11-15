/**
 * Minimal typings for the official `telegram-web-app.js` script.
 *
 * The package is not imported: the script is loaded from telegram.org in
 * `index.html` as the Telegram documentation requires, and its absence (a
 * plain browser) is the normal "browser" host. Defining only the members the
 * WebApp uses keeps the bundle free of a runtime dependency on the SDK.
 */

/** Telegram theme parameters as sent by the client (`snake_case` keys). */
export type TelegramThemeParams = Record<string, string | undefined>;

/** Subset of `Telegram.WebApp` used by the WebApp. */
export interface TelegramWebApp {
  initData: string;
  initDataUnsafe?: {
    user?: { id: number; first_name?: string; last_name?: string; username?: string };
  };
  colorScheme: "light" | "dark";
  themeParams: TelegramThemeParams;
  ready(): void;
  expand(): void;
  close(): void;
  openLink(url: string, options?: { try_instant_view?: boolean }): void;
  showAlert(message: string, callback?: () => void): void;
  showConfirm(message: string, callback?: (confirmed: boolean) => void): void;
  onEvent(event: string, handler: () => void): void;
  offEvent(event: string, handler: () => void): void;
  HapticFeedback?: {
    selectionChanged(): void;
    impactOccurred(style: "light" | "medium" | "heavy" | "rigid" | "soft"): void;
    notificationOccurred(type: "success" | "warning" | "error"): void;
  };
  BackButton?: {
    show(): void;
    hide(): void;
    onClick(handler: () => void): void;
    offClick(handler: () => void): void;
  };
}

declare global {
  interface Window {
    Telegram?: { WebApp?: TelegramWebApp };
  }
}
