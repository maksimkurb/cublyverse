import { useEffect, useMemo, useState } from "react";

import type { Workspace } from "../../api/client";
import type { SpaceApp } from "./apps";
import { useWorkspacesQuery } from "./data";

/** Key of the space of a mini-app chosen in its switcher. */
export function workspaceStorageKey(app: SpaceApp): string {
  return `cv.space.${app}`;
}

/** Reads the remembered space id; storage may be unavailable. */
export function readStoredWorkspaceID(app: SpaceApp): string | null {
  try {
    return window.localStorage.getItem(workspaceStorageKey(app));
  } catch {
    return null;
  }
}

/** Remembers the chosen space; a refusal is not an error worth showing. */
export function writeStoredWorkspaceID(app: SpaceApp, id: string | null): void {
  try {
    if (id) {
      window.localStorage.setItem(workspaceStorageKey(app), id);
    } else {
      window.localStorage.removeItem(workspaceStorageKey(app));
    }
  } catch {
    // Private mode or a full quota: the switcher keeps working in memory.
  }
}

/** Result of the shared workspace selection. */
export interface CurrentWorkspace {
  /** The workspace list request, for loading and error states. */
  query: ReturnType<typeof useWorkspacesQuery>;
  workspaces: Workspace[];
  /** The chosen workspace, or null while the list is empty or loading. */
  current: Workspace | null;
  /** Chooses a workspace and remembers it. */
  select: (id: string) => void;
}

/**
 * Selection of the current space of a mini-app. The id is persisted in
 * `localStorage`, but an id that is no longer in the list (deleted space,
 * another account) falls back to the first one.
 */
export function useCurrentWorkspace(app: SpaceApp): CurrentWorkspace {
  const query = useWorkspacesQuery(app);
  const workspaces = useMemo(() => query.data ?? [], [query.data]);
  const [selected, setSelected] = useState<string | null>(() => readStoredWorkspaceID(app));

  useEffect(() => {
    if (workspaces.length === 0) {
      return;
    }
    if (selected && workspaces.some((workspace) => workspace.id === selected)) {
      return;
    }
    const first = workspaces[0].id;
    setSelected(first);
    writeStoredWorkspaceID(app, first);
  }, [app, workspaces, selected]);

  function select(id: string) {
    setSelected(id);
    writeStoredWorkspaceID(app, id);
  }

  const current = workspaces.find((workspace) => workspace.id === selected) ?? null;
  return { query, workspaces, current, select };
}
