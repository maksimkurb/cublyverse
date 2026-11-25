import type { FeatureScreen } from "../types";
import { IconUsers } from "../../ui/icons";
import MembersScreen from "./MembersScreen";

/** Shared workspaces: who is in one, who can be invited, create and rename. */
export const workspacesFeature: FeatureScreen = {
  id: "workspaces",
  title: "Участники",
  icon: IconUsers,
  route: "/workspaces",
  component: MembersScreen,
};
