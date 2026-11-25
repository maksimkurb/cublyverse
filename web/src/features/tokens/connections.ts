import { useQueryClient } from "@tanstack/react-query";

import { $api } from "../../api/query";

/** Query key of `GET /connections`, as built by openapi-react-query. */
export const connectionsQueryKey = ["get", "/connections"] as const;

/** Loads the applications connected through OAuth. */
export function useConnectionsQuery() {
  return $api.useQuery("get", "/connections");
}

/** Disconnects one application; the list is refreshed. */
export function useRevokeConnection() {
  const queryClient = useQueryClient();
  return $api.useMutation("delete", "/connections/{id}", {
    onSuccess: () => queryClient.invalidateQueries({ queryKey: connectionsQueryKey }),
  });
}
