import type { FeatureScreen } from "../types";
import { IconBox, IconLayers } from "../../ui/icons";
import CraftScreen from "./CraftScreen";
import ProjectsScreen from "./ProjectsScreen";

/** Craft supplies: stock, purchases and catalogue of the selected workspace. */
export const craftFeature: FeatureScreen = {
  id: "craft",
  title: "Запасы",
  icon: IconBox,
  route: "/craft",
  component: CraftScreen,
};

/** Craft projects of the selected workspace and their material plans. */
export const projectsFeature: FeatureScreen = {
  id: "projects",
  title: "Проекты",
  icon: IconLayers,
  route: "/projects",
  component: ProjectsScreen,
};
