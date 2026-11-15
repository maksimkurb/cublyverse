import type { FeatureScreen } from "../types";
import TokensScreen from "./TokensScreen";

/** Personal access tokens: list, create once, revoke in two taps. */
export const tokensFeature: FeatureScreen = {
  id: "tokens",
  title: "Доступ по токенам",
  icon: "🔑",
  route: "/tokens",
  component: TokensScreen,
};
