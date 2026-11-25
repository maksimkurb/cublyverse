import { useQueryClient, type QueryClient } from "@tanstack/react-query";

import { $api } from "../../api/query";
import type { SpaceApp } from "./apps";

/** Query key of `GET /workspaces`, as built by openapi-react-query. */
export const workspacesQueryKey = ["get", "/workspaces"] as const;

/**
 * Refreshes everything that lives inside a workspace. The path of the
 * operation is the second element of the key, so a prefix match covers the
 * workspace list, the members, the catalogue and the stock at once.
 */
function invalidateWorkspaceData(queryClient: QueryClient) {
  return queryClient.invalidateQueries({
    predicate: (query) => typeof query.queryKey[1] === "string" && query.queryKey[1].startsWith("/workspaces"),
  });
}

/** Loads the spaces of one mini-app the signed-in user belongs to. */
export function useWorkspacesQuery(app: SpaceApp) {
  return $api.useQuery("get", "/workspaces", { params: { query: { app } } });
}

/** Creates a workspace with the user as its first member. */
export function useCreateWorkspace() {
  const queryClient = useQueryClient();
  return $api.useMutation("post", "/workspaces", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** Renames a workspace. */
export function useRenameWorkspace() {
  const queryClient = useQueryClient();
  return $api.useMutation("patch", "/workspaces/{id}", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** Deletes a workspace with everything in it. */
export function useDeleteWorkspace() {
  const queryClient = useQueryClient();
  return $api.useMutation("delete", "/workspaces/{id}", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** Loads the members of one workspace. */
export function useWorkspaceMembers(workspaceId: string) {
  return $api.useQuery("get", "/workspaces/{id}/members", { params: { path: { id: workspaceId } } });
}

/** Adds an existing user; adding a member twice is a no-op. */
export function useAddWorkspaceMember() {
  const queryClient = useQueryClient();
  return $api.useMutation("post", "/workspaces/{id}/members", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** Removes a member; a user may remove themselves. */
export function useRemoveWorkspaceMember() {
  const queryClient = useQueryClient();
  return $api.useMutation("delete", "/workspaces/{id}/members/{user_id}", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** Loads users who can still be added, filtered by name or e-mail. */
export function useWorkspaceCandidates(workspaceId: string, query: string) {
  return $api.useQuery("get", "/workspaces/{id}/member-candidates", {
    params: { path: { id: workspaceId }, query: { query: query.trim() || undefined } },
  });
}
