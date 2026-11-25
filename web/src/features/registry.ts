import type { FeatureScreen } from "./types";
import { craftFeature } from "./craft";
import { remindersFeature } from "./reminders";
import { tokensFeature } from "./tokens";
import { workspacesFeature } from "./workspaces";

/**
 * Feature registry.
 *
 * Adding a feature screen is: create `web/src/features/<name>/index.ts` that
 * exports a `FeatureScreen`, then add it to this array. The navigation lists
 * it and the router renders its `component` on its `route`; no other file
 * changes. The first feature is what a bare `/app/` opens.
 */
export const features: FeatureScreen[] = [remindersFeature, craftFeature, tokensFeature, workspacesFeature];
