import { useQueryClient, type QueryClient } from "@tanstack/react-query";

import type { CraftItemInput, WishStatus } from "../../api/client";
import { $api } from "../../api/query";

/** Filter of a catalogue or stock list. */
export interface CraftFilter {
  category?: string;
  manufacturer?: string;
  /** Article prefix, or part of the item or colour name. */
  q?: string;
  /** Catalogue only: items the workspace holds. */
  onlyInStock?: boolean;
  /** Stock only: rows with quantity at most this value. */
  maxQty?: number;
  limit?: number;
  offset?: number;
}

/** Refreshes every operation that lives under a workspace. */
function invalidateWorkspaceData(queryClient: QueryClient) {
  return queryClient.invalidateQueries({
    predicate: (query) => typeof query.queryKey[1] === "string" && query.queryKey[1].startsWith("/workspaces"),
  });
}

/** Material categories with their default units. */
export function useCraftCategories() {
  return $api.useQuery("get", "/craft/categories");
}

/** Known manufacturers. */
export function useCraftManufacturers() {
  return $api.useQuery("get", "/craft/manufacturers");
}

/** Search the shared catalogue and the private items of the workspace. */
export function useCraftCatalog(workspaceId: string, filter: CraftFilter) {
  return $api.useQuery("get", "/workspaces/{id}/catalog", {
    params: {
      path: { id: workspaceId },
      query: {
        category: filter.category || undefined,
        manufacturer: filter.manufacturer || undefined,
        q: filter.q?.trim() || undefined,
        only_in_stock: filter.onlyInStock || undefined,
        limit: filter.limit,
        offset: filter.offset,
      },
    },
  });
}

/** The stock rows of the workspace. */
export function useCraftInventory(workspaceId: string, filter: CraftFilter) {
  return $api.useQuery("get", "/workspaces/{id}/inventory", {
    params: {
      path: { id: workspaceId },
      query: {
        category: filter.category || undefined,
        manufacturer: filter.manufacturer || undefined,
        q: filter.q?.trim() || undefined,
        max_qty: filter.maxQty,
        limit: filter.limit,
        offset: filter.offset,
      },
    },
  });
}

/** Creates a private catalogue item. */
export function useCreateCraftItem() {
  const queryClient = useQueryClient();
  return $api.useMutation("post", "/workspaces/{id}/catalog", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** Adds to or uses up the stock by a delta. */
export function useAdjustCraftStock() {
  const queryClient = useQueryClient();
  return $api.useMutation("post", "/workspaces/{id}/inventory/{item_id}/adjust", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** Sets the stock to an absolute value. */
export function useSetCraftStock() {
  const queryClient = useQueryClient();
  return $api.useMutation("put", "/workspaces/{id}/inventory/{item_id}", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** Changes the location and notes of a stock row. */
export function useUpdateCraftStockMeta() {
  const queryClient = useQueryClient();
  return $api.useMutation("patch", "/workspaces/{id}/inventory/{item_id}", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** Removes an item from stock; the catalogue item stays. */
export function useRemoveCraftStock() {
  const queryClient = useQueryClient();
  return $api.useMutation("delete", "/workspaces/{id}/inventory/{item_id}", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** Moves the stock of a private item to a catalogue item. */
export function useLinkCraftItem() {
  const queryClient = useQueryClient();
  return $api.useMutation("post", "/workspaces/{id}/catalog/{item_id}/link", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** The wish list of the workspace; `all` returns every status. */
export function useWishes(workspaceId: string, status: WishStatus | "all" = "all") {
  return $api.useQuery("get", "/workspaces/{id}/wishlist", {
    params: { path: { id: workspaceId }, query: { status } },
  });
}

/** Adds a wish list entry: a catalogue item or free text. */
export function useCreateWish() {
  const queryClient = useQueryClient();
  return $api.useMutation("post", "/workspaces/{id}/wishlist", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** Replaces the quantity, priority, note and status of a wish. */
export function useUpdateWish() {
  const queryClient = useQueryClient();
  return $api.useMutation("patch", "/workspaces/{id}/wishlist/{wish_id}", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** Deletes a wish with its shop links. */
export function useDeleteWish() {
  const queryClient = useQueryClient();
  return $api.useMutation("delete", "/workspaces/{id}/wishlist/{wish_id}", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** Attaches a shop page to a wish. */
export function useAddWishLink() {
  const queryClient = useQueryClient();
  return $api.useMutation("post", "/workspaces/{id}/wishlist/{wish_id}/links", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** Removes one shop page of a wish. */
export function useRemoveWishLink() {
  const queryClient = useQueryClient();
  return $api.useMutation("delete", "/workspaces/{id}/wishlist/{wish_id}/links/{link_id}", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** Marks a wish bought, optionally adding the purchase to the stock. */
export function useMarkWishBought() {
  const queryClient = useQueryClient();
  return $api.useMutation("post", "/workspaces/{id}/wishlist/{wish_id}/bought", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** The projects of the workspace, without their materials. */
export function useProjects(workspaceId: string) {
  return $api.useQuery("get", "/workspaces/{id}/projects", {
    params: { path: { id: workspaceId } },
  });
}

/** One project with its materials, stock and shortage. */
export function useProject(workspaceId: string, projectId: string) {
  return $api.useQuery("get", "/workspaces/{id}/projects/{project_id}", {
    params: { path: { id: workspaceId, project_id: projectId } },
  });
}

/** Creates an empty craft project. */
export function useCreateProject() {
  const queryClient = useQueryClient();
  return $api.useMutation("post", "/workspaces/{id}/projects", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** Replaces the name and notes of a project. */
export function useUpdateProject() {
  const queryClient = useQueryClient();
  return $api.useMutation("patch", "/workspaces/{id}/projects/{project_id}", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** Deletes a project with its material plan. */
export function useDeleteProject() {
  const queryClient = useQueryClient();
  return $api.useMutation("delete", "/workspaces/{id}/projects/{project_id}", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** Sets or clears the planned quantity of a project material. */
export function useSetProjectMaterial() {
  const queryClient = useQueryClient();
  return $api.useMutation("put", "/workspaces/{id}/projects/{project_id}/materials/{item_id}", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** Drops a material from the project plan. */
export function useRemoveProjectMaterial() {
  const queryClient = useQueryClient();
  return $api.useMutation("delete", "/workspaces/{id}/projects/{project_id}/materials/{item_id}", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** Withdraws stock for a project and records the usage. */
export function useUseProjectMaterial() {
  const queryClient = useQueryClient();
  return $api.useMutation("post", "/workspaces/{id}/projects/{project_id}/materials/{item_id}/use", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** Adds the project shortage to the wish list. */
export function useShortageToWishlist() {
  const queryClient = useQueryClient();
  return $api.useMutation("post", "/workspaces/{id}/projects/{project_id}/shortage-to-wishlist", {
    onSuccess: () => invalidateWorkspaceData(queryClient),
  });
}

/** Request body of a create call; the screen builds it from its form. */
export type CraftItemBody = CraftItemInput;
