import type { ComponentType } from "react";

/**
 * One feature screen. Adding a feature means adding
 * `web/src/features/<name>/index.ts` that exports such an object and one line in
 * `registry.ts`; the navigation and the router pick it up automatically.
 */
export interface FeatureScreen {
  /** Stable identifier, also used as a key. */
  id: string;
  /** Short Russian title shown in the navigation. */
  title: string;
  /** Navigation icon: an inline SVG component from `ui/icons`. */
  icon: ComponentType<{ size?: number }>;
  /** Route under `/app`, for example `/reminders`. */
  route: string;
  /** Screen component. */
  component: ComponentType;
}
