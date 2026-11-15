import { createContext, useContext, useEffect, useRef, useState } from "react";

import { host as defaultHost } from "./index";
import type { HostBridge, HostTheme } from "./types";

/** The host bridge of the current page. Tests provide a fake. */
export const HostContext = createContext<HostBridge>(defaultHost);

/** Returns the host bridge. */
export function useHost(): HostBridge {
  return useContext(HostContext);
}

/** Tracks the host colour scheme, including later changes. */
export function useHostTheme(): HostTheme {
  const host = useHost();
  const [theme, setTheme] = useState<HostTheme>(host.theme);

  useEffect(() => {
    setTheme(host.theme);
    return host.onThemeChange(setTheme);
  }, [host]);

  return theme;
}

/**
 * Back button handlers, newest last. The host shows one button, so nested
 * screens (a modal over a list) share it: the most recent handler wins and the
 * previous one takes over when the modal unmounts.
 */
const backStack: Array<() => void> = [];
let unsubscribeBack: (() => void) | null = null;

/** Routes the host back button to `handler` while the component is mounted. */
export function useHostBackButton(handler: () => void): void {
  const host = useHost();
  const handlerRef = useRef(handler);
  handlerRef.current = handler;

  useEffect(() => {
    const entry = () => handlerRef.current();
    backStack.push(entry);
    if (!unsubscribeBack) {
      unsubscribeBack = host.backButton.onClick(() => backStack[backStack.length - 1]?.());
    }
    host.backButton.show();

    return () => {
      const index = backStack.lastIndexOf(entry);
      if (index >= 0) {
        backStack.splice(index, 1);
      }
      if (backStack.length === 0) {
        unsubscribeBack?.();
        unsubscribeBack = null;
        host.backButton.hide();
      }
    };
  }, [host]);
}
