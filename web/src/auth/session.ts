/**
 * In-memory session state.
 *
 * The Bearer token of the Telegram Mini App never touches `localStorage`: it
 * lives only in this module for the lifetime of the page. The browser session
 * is an `HttpOnly` cookie and is not represented here at all, so `getToken()`
 * returning `null` means "use the cookie".
 */

let token: string | null = null;
let refresher: (() => Promise<string | null>) | null = null;
let refreshPromise: Promise<string | null> | null = null;
let unauthorizedHandler: (() => void) | null = null;

/** Current Bearer token, or `null`. */
export function getSessionToken(): string | null {
  return token;
}

/** Stores a Bearer token, or clears it with `null`. */
export function setSessionToken(value: string | null): void {
  token = value;
}

/**
 * Registers the function that obtains a fresh Telegram session. It is called at
 * most once per burst of 401 answers.
 */
export function setSessionRefresher(fn: (() => Promise<string | null>) | null): void {
  refresher = fn;
}

/** Called when a request stayed unauthorized even after a refresh attempt. */
export function setUnauthorizedHandler(handler: (() => void) | null): void {
  unauthorizedHandler = handler;
}

/** Notifies the UI that the session is gone. */
export function notifyUnauthorized(): void {
  unauthorizedHandler?.();
}

/**
 * Refreshes the Bearer token once, sharing the in-flight call between
 * concurrent requests. Resolves to `null` when no refresh is possible or it
 * failed; callers then surface the original 401.
 */
export function refreshSessionToken(): Promise<string | null> {
  if (!refresher) {
    return Promise.resolve(null);
  }
  if (!refreshPromise) {
    refreshPromise = refresher()
      .then((value) => {
        if (value) {
          token = value;
        }
        return value;
      })
      .catch(() => null)
      .finally(() => {
        refreshPromise = null;
      });
  }
  return refreshPromise;
}

/** Clears every piece of session state (used on logout and in tests). */
export function resetSessionState(): void {
  token = null;
  refresher = null;
  refreshPromise = null;
  unauthorizedHandler = null;
}
