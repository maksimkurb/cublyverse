import type { FeatureScreen } from "../types";
import { IconBox } from "../../ui/icons";
import CraftScreen from "./CraftScreen";

/** Craft supplies: stock and catalogue of the selected workspace. */
export const craftFeature: FeatureScreen = {
  id: "craft",
  title: "Запасы",
  icon: IconBox,
  route: "/craft",
  component: CraftScreen,
};
