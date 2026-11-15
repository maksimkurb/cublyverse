import type { ComponentType } from "react";

/**
 * One feature screen. Adding a feature means adding
 * `web/src/features/<name>/index.ts` that exports such an object and one line in
 * `registry.ts`; the home screen and the router pick it up automatically.
 */
export interface FeatureScreen {
  /** Stable identifier, also used as a key. */
  id: string;
  /** Russian title shown in menus and as the page heading. */
  title: string;
  /** Short icon: an emoji keeps the bundle free of an icon dependency. */
  icon: string;
  /** Hash route, for example `/reminders`. */
  route: string;
  /** Screen component. */
  component: ComponentType;
}
