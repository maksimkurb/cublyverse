import type { FeatureScreen } from "../types";
import { IconBox, IconHeart, IconLayers } from "../../ui/icons";
import CraftScreen from "./CraftScreen";
import ProjectsScreen from "./ProjectsScreen";
import WishlistScreen from "./WishlistScreen";

/** Craft supplies: stock and catalogue of the selected workspace. */
export const craftFeature: FeatureScreen = {
  id: "craft",
  title: "Запасы",
  icon: IconBox,
  route: "/craft",
  component: CraftScreen,
};

/** The wish list of the selected workspace, grouped by status. */
export const wishlistFeature: FeatureScreen = {
  id: "wishlist",
  title: "Wishlist",
  icon: IconHeart,
  route: "/wishlist",
  component: WishlistScreen,
};

/** Craft projects of the selected workspace and their material plans. */
export const projectsFeature: FeatureScreen = {
  id: "projects",
  title: "Проекты",
  icon: IconLayers,
  route: "/projects",
  component: ProjectsScreen,
};
