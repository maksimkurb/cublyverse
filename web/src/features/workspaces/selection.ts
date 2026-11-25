import { useEffect, useMemo, useState } from "react";

import type { Workspace } from "../../api/client";
import { useWorkspacesQuery } from "./data";

/** Key of the workspace chosen in the switcher. */
export const WORKSPACE_STORAGE_KEY = "cv.workspace";

/** Reads the remembered workspace id; storage may be unavailable. */
export function readStoredWorkspaceID(): string | null {
  try {
    return window.localStorage.getItem(WORKSPACE_STORAGE_KEY);
  } catch {
    return null;
  }
}

/** Remembers the chosen workspace; a refusal is not an error worth showing. */
export function writeStoredWorkspaceID(id: string | null): void {
  try {
    if (id) {
      window.localStorage.setItem(WORKSPACE_STORAGE_KEY, id);
    } else {
      window.localStorage.removeItem(WORKSPACE_STORAGE_KEY);
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
 * Shared selection of the current workspace. The id is persisted in
 * `localStorage`, but an id that is no longer in the list (deleted workspace,
 * another account) falls back to the first one.
 */
export function useCurrentWorkspace(): CurrentWorkspace {
  const query = useWorkspacesQuery();
  const workspaces = useMemo(() => query.data ?? [], [query.data]);
  const [selected, setSelected] = useState<string | null>(() => readStoredWorkspaceID());

  useEffect(() => {
    if (workspaces.length === 0) {
      return;
    }
    if (selected && workspaces.some((workspace) => workspace.id === selected)) {
      return;
    }
    const first = workspaces[0].id;
    setSelected(first);
    writeStoredWorkspaceID(first);
  }, [workspaces, selected]);

  function select(id: string) {
    setSelected(id);
    writeStoredWorkspaceID(id);
  }

  const current = workspaces.find((workspace) => workspace.id === selected) ?? null;
  return { query, workspaces, current, select };
}
