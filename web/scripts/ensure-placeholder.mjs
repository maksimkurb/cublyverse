// Vite empties dist on every build, but the Go embed directive needs at least
// one file so `go build` works on a fresh checkout. Re-create the tracked
// placeholder after each build.
import { mkdirSync, writeFileSync } from "node:fs";

mkdirSync("dist", { recursive: true });
writeFileSync("dist/.gitkeep", "");
