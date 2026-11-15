import { useEffect, useSyncExternalStore, type AnchorHTMLAttributes, type ReactNode } from "react";

/**
 * Tiny history router under the `/app/` base.
 *
 * The Go binary already serves `index.html` for any `/app/*` path, so real URLs
 * work on reload and on a deep link. History routing (instead of a hash) also
 * keeps Telegram's own bootstrap fragment (`#tgWebAppData=…`) untouched, which a
 * Mini App reload needs.
 */

const BASE = "/app";
const HOME_ROUTE = "/";
const NAVIGATE_EVENT = "cv:navigate";

function readRoute(): string {
  const path = window.location.pathname;
  if (path === BASE || path === `${BASE}/`) {
    return HOME_ROUTE;
  }
  if (path.startsWith(`${BASE}/`)) {
    const route = path.slice(BASE.length) || HOME_ROUTE;
    return route.length > 1 && route.endsWith("/") ? route.slice(0, -1) : route;
  }
  return HOME_ROUTE;
}

function subscribe(listener: () => void): () => void {
  window.addEventListener("popstate", listener);
  window.addEventListener(NAVIGATE_EVENT, listener);
  return () => {
    window.removeEventListener("popstate", listener);
    window.removeEventListener(NAVIGATE_EVENT, listener);
  };
}

/** Current route, for example `/` or `/reminders`. */
export function useRoute(): string {
  return useSyncExternalStore(subscribe, readRoute, () => HOME_ROUTE);
}

/** Navigates to a route by pushing a browser history entry. */
export function navigate(route: string): void {
  const target = route.startsWith("/") ? route : `/${route}`;
  if (readRoute() === target) {
    return;
  }
  window.history.pushState(null, "", `${BASE}${target === "/" ? "/" : target}`);
  // pushState does not fire popstate, so tell the router that it changed.
  window.dispatchEvent(new Event(NAVIGATE_EVENT));
}

/** Props of an in-app link. */
export interface LinkProps extends Omit<AnchorHTMLAttributes<HTMLAnchorElement>, "href"> {
  to: string;
  children: ReactNode;
}

/** Anchor that keeps the page alive: it navigates through history. */
export function Link({ to, children, onClick, ...rest }: LinkProps) {
  return (
    <a
      href={`${BASE}${to}`}
      onClick={(event) => {
        onClick?.(event);
        if (!event.defaultPrevented && event.button === 0 && !event.metaKey && !event.ctrlKey) {
          event.preventDefault();
          navigate(to);
        }
      }}
      {...rest}
    >
      {children}
    </a>
  );
}

/** Sets `document.title` while a screen is mounted. */
export function usePageTitle(title: string): void {
  useEffect(() => {
    document.title = title;
  }, [title]);
}
