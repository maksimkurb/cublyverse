/**
 * Host bridge: the small surface of the embedding environment the WebApp
 * needs. The UI never calls `Telegram.WebApp` or `window.confirm` directly, so
 * the same components run in the Telegram Mini App, inside a Matrix widget and
 * in a plain browser.
 */

/** Where the WebApp is running. */
export type HostKind = "telegram" | "matrix" | "browser";

/** Colour scheme of the host. */
export type HostTheme = "light" | "dark";

/** Kinds of haptic feedback the UI asks for. */
export type HapticKind = "selection" | "success" | "warning" | "error";

/** Shows a modal message; resolves when the user dismisses it. */
export type ShowAlert = (message: string) => Promise<void>;

/** Asks a yes/no question; resolves to the user's answer. */
export type ShowConfirm = (message: string) => Promise<boolean>;

/** The host back button. Hidden by default. */
export interface BackButton {
  show(): void;
  hide(): void;
  onClick(listener: () => void): () => void;
}

/** Everything the WebApp expects from its host. */
export interface HostBridge {
  /** Detected host. `matrix` currently behaves like `browser`. */
  readonly kind: HostKind;
  /**
   * True for the Telegram Mini App. The UI uses it to keep the session token in
   * memory and to render the Telegram-only pieces.
   */
  readonly isTelegram: boolean;
  /** Current colour scheme. */
  readonly theme: HostTheme;
  /** Telegram `initData`, or an empty string outside Telegram. */
  readonly initData: string;

  /** Prepares the host: theme variables, `ready`/`expand` for Telegram. */
  init(): void;
  /** Subscribes to theme changes; returns the unsubscribe function. */
  onThemeChange(listener: (theme: HostTheme) => void): () => void;
  /** Plays haptic feedback when the host supports it. */
  haptic(kind: HapticKind): void;
  showAlert: ShowAlert;
  showConfirm: ShowConfirm;
  /**
   * Opens a link. A same-origin URL (including a relative path) is opened in
   * the current webview, which keeps Telegram `initData` available on the next
   * page; anything else goes to the host's external browser.
   */
  openLink(url: string): void;
  /** Closes the Mini App / window. */
  close(): void;
  /** Back button of the host. */
  readonly backButton: BackButton;
}
