import { useQueryClient, type QueryClient } from "@tanstack/react-query";

import type { CraftItemInput } from "../../api/client";
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

/** Request body of a create call; the screen builds it from its form. */
export type CraftItemBody = CraftItemInput;
