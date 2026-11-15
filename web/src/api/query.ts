import createQueryClient from "openapi-react-query";

import { api } from "./client";

/**
 * The API client in react-query form. Hooks keep the generated request and
 * response types, so a change in `api/openapi.yaml` breaks the UI at compile
 * time.
 */
export const $api = createQueryClient(api);
