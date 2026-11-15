import { useMemo } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AppRoot } from "@telegram-apps/telegram-ui";

import { AuthProvider, type AuthDeps } from "./auth/AuthProvider";
import { features } from "./features/registry";
import HomeScreen from "./HomeScreen";
import { HostContext, useHostTheme } from "./host/HostContext";
import { host as defaultHost } from "./host/index";
import type { HostBridge } from "./host/types";
import ProfileScreen from "./profile/ProfileScreen";
import { useRoute } from "./router/router";

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
  const feature = features.find((item) => item.route === route);
  if (feature) {
    const Screen = feature.component;
    return <Screen />;
  }
  if (route === "/profile") {
    return <ProfileScreen />;
  }
  return <HomeScreen />;
}

function ThemedShell({ authDeps }: { authDeps?: AuthDeps }) {
  const theme = useHostTheme();
  return (
    <AppRoot appearance={theme} className="cv-root">
      <AuthProvider deps={authDeps}>
        <RoutedScreen />
      </AuthProvider>
    </AppRoot>
  );
}

/**
 * WebApp root: query cache, host bridge, telegram-ui root (theme + portals),
 * auth gate and router.
 */
export default function App({ host = defaultHost, queryClient, authDeps }: AppProps) {
  const client = useMemo(() => queryClient ?? createQueryClient(), [queryClient]);

  return (
    <QueryClientProvider client={client}>
      <HostContext.Provider value={host}>
        <ThemedShell authDeps={authDeps} />
      </HostContext.Provider>
    </QueryClientProvider>
  );
}
