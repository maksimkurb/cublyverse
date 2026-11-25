import type { FeatureScreen } from "../types";
import { IconBox } from "../../ui/icons";
import CraftScreen from "./CraftScreen";

/** «Склад»: stock, purchases, projects and catalogue of the selected workspace. */
export const craftFeature: FeatureScreen = {
  id: "craft",
  title: "Склад",
  icon: IconBox,
  route: "/craft",
  component: CraftScreen,
};
