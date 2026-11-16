import type { FeatureScreen } from "../types";
import { IconKey } from "../../ui/icons";
import TokensScreen from "./TokensScreen";

/** Personal access tokens: list, create once, revoke in two taps. */
export const tokensFeature: FeatureScreen = {
  id: "tokens",
  title: "Токены",
  icon: IconKey,
  route: "/tokens",
  component: TokensScreen,
};
