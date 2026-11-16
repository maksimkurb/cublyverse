import { useMemo } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { AuthProvider, type AuthDeps } from "./auth/AuthProvider";
import { features } from "./features/registry";
import { HostContext } from "./host/HostContext";
import { host as defaultHost } from "./host/index";
import type { HostBridge } from "./host/types";
import ProfileScreen from "./profile/ProfileScreen";
import { useRoute } from "./router/router";
import { AppShell, homeRoute } from "./ui/AppShell";

/** Query client shared by every screen. */
export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        refetchOnWindowFocus: false,
        staleTime: 30_000,
      },
    },
  });
}

/** Props of the application root; tests inject fakes. */
export interface AppProps {
  host?: HostBridge;
  queryClient?: QueryClient;
  authDeps?: AuthDeps;
}

function RoutedScreen() {
  const route = useRoute();
  const current = route === "/" ? homeRoute : route;
  const feature = features.find((item) => item.route === current) ?? features[0];
  const Screen = current === "/profile" ? ProfileScreen : feature.component;

  return (
    <AppShell route={route}>
      <Screen />
    </AppShell>
  );
}

/**
 * WebApp root: query cache, host bridge, auth gate and the routed shell. The
 * colour scheme is the `data-theme` attribute the host bridge keeps on
 * `<html>`.
 */
export default function App({ host = defaultHost, queryClient, authDeps }: AppProps) {
  const client = useMemo(() => queryClient ?? createQueryClient(), [queryClient]);

  return (
    <QueryClientProvider client={client}>
      <HostContext.Provider value={host}>
        <div className="cv-root">
          <AuthProvider deps={authDeps}>
            <RoutedScreen />
          </AuthProvider>
        </div>
      </HostContext.Provider>
    </QueryClientProvider>
  );
}
