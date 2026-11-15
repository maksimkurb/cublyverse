import type { FeatureScreen } from "./types";
import { remindersFeature } from "./reminders";

/**
 * Feature registry.
 *
 * Adding a feature screen is: create `web/src/features/<name>/index.ts` that
 * exports a `FeatureScreen`, then add it to this array. The home screen lists it
 * and the router renders its `component` on its `route`; no other file changes.
 */
export const features: FeatureScreen[] = [remindersFeature];
