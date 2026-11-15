import { useQueryClient } from "@tanstack/react-query";

import type { CreatedToken } from "../../api/client";
import { $api } from "../../api/query";

/** Query key of `GET /tokens`, as built by openapi-react-query. */
export const tokensQueryKey = ["get", "/tokens"] as const;

/** Loads every personal access token of the signed-in user. */
export function useTokensQuery() {
  return $api.useQuery("get", "/tokens");
}

/** Creates one token; the answer carries the plaintext exactly once. */
export function useCreateToken() {
  const queryClient = useQueryClient();
  return $api.useMutation("post", "/tokens", {
    onSuccess: () => queryClient.invalidateQueries({ queryKey: tokensQueryKey }),
  });
}

/** Revokes one token; the list is refreshed so the row shows the new state. */
export function useRevokeToken() {
  const queryClient = useQueryClient();
  return $api.useMutation("delete", "/tokens/{id}", {
    onSuccess: () => queryClient.invalidateQueries({ queryKey: tokensQueryKey }),
  });
}

/** Request body of one create call. */
export type TokenCreateBody = CreatedToken;
