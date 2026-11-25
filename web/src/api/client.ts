import createClient from "openapi-fetch";

import { createAuthMiddleware } from "./authMiddleware";
import type { components, paths } from "./schema";

/** Payload types generated from `api/openapi.yaml`. */
export type Me = components["schemas"]["Me"];
export type Reminder = components["schemas"]["Reminder"];
export type ReminderOccurrence = components["schemas"]["ReminderOccurrence"];
export type CreateReminderRequest = components["schemas"]["CreateReminderRequest"];
export type UpdateReminderRequest = components["schemas"]["UpdateReminderRequest"];
export type TelegramAuthResponse = components["schemas"]["TelegramAuthResponse"];
export type APIToken = components["schemas"]["APIToken"];
export type Connection = components["schemas"]["Connection"];
export type APITokenScope = components["schemas"]["APITokenScope"];
export type CreateTokenRequest = components["schemas"]["CreateTokenRequest"];
export type CreatedToken = components["schemas"]["CreatedToken"];
export type Workspace = components["schemas"]["Workspace"];
export type WorkspaceMember = components["schemas"]["WorkspaceMember"];
export type WorkspaceCandidate = components["schemas"]["WorkspaceCandidate"];
export type CreateWorkspaceRequest = components["schemas"]["CreateWorkspaceRequest"];
export type RenameWorkspaceRequest = components["schemas"]["RenameWorkspaceRequest"];
export type AddWorkspaceMemberRequest = components["schemas"]["AddWorkspaceMemberRequest"];
export type CraftCategory = components["schemas"]["CraftCategory"];
export type CraftManufacturer = components["schemas"]["CraftManufacturer"];
export type CraftUnit = components["schemas"]["CraftUnit"];
export type CraftField = components["schemas"]["CraftField"];
export type CraftReference = components["schemas"]["CraftReference"];
export type CraftItem = components["schemas"]["CraftItem"];
export type CraftItemInput = components["schemas"]["CraftItemInput"];
export type CraftStockEntry = components["schemas"]["CraftStockEntry"];
export type SetCraftStockRequest = components["schemas"]["SetCraftStockRequest"];
export type AdjustCraftStockRequest = components["schemas"]["AdjustCraftStockRequest"];
export type UpdateCraftStockMetaRequest = components["schemas"]["UpdateCraftStockMetaRequest"];
export type LinkCraftItemRequest = components["schemas"]["LinkCraftItemRequest"];
export type Wish = components["schemas"]["Wish"];
export type WishLink = components["schemas"]["WishLink"];
export type WishStatus = components["schemas"]["WishStatus"];
export type WishInput = components["schemas"]["WishInput"];
export type WishUpdateRequest = components["schemas"]["WishUpdateRequest"];
export type WishLinkInput = components["schemas"]["WishLinkInput"];
export type MarkWishBoughtRequest = components["schemas"]["MarkWishBoughtRequest"];
export type Project = components["schemas"]["Project"];
export type ProjectMaterial = components["schemas"]["ProjectMaterial"];
export type ProjectInput = components["schemas"]["ProjectInput"];
export type SetProjectMaterialRequest = components["schemas"]["SetProjectMaterialRequest"];
export type UseProjectMaterialRequest = components["schemas"]["UseProjectMaterialRequest"];
export type ApiError = components["schemas"]["Error"];

/** Fetch implementation accepted by the client factory (injectable in tests). */
export type FetchLike = (request: Request) => Promise<Response>;

/**
 * Builds a typed API client.
 *
 * URLs are relative to the page origin: the Go binary serves the WebApp and the
 * API together, and the Vite dev server proxies `/api` to the backend.
 */
export function createApiClient(fetchImpl: FetchLike = (request) => globalThis.fetch(request), baseUrl = "/api/v1") {
  const client = createClient<paths>({ baseUrl, fetch: fetchImpl });
  client.use(createAuthMiddleware(fetchImpl));
  return client;
}

/** Typed client for the Cublyverse HTTP API. */
export const api = createApiClient();
