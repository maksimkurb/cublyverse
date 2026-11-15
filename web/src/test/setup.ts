import { cleanup } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { afterEach } from "vitest";

// Vitest runs without globals, so React Testing Library cannot clean up on its
// own.
afterEach(() => {
  cleanup();
});
