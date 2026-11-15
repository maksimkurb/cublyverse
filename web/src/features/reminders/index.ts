import type { FeatureScreen } from "../types";
import RemindersScreen from "./RemindersScreen";

/** The reminders feature: list + modal editor over the reminders API. */
export const remindersFeature: FeatureScreen = {
  id: "reminders",
  title: "Напоминания",
  icon: "⏰",
  route: "/reminders",
  component: RemindersScreen,
};
