import { render, type RenderResult } from "@testing-library/react";
import type { ReactElement } from "react";

import { HostContext } from "../host/HostContext";
import type { HostBridge } from "../host/types";
import { makeFakeHost } from "./fakeHost";

/** Renders a component with a fake host. */
export function renderWithHost(ui: ReactElement, host: HostBridge = makeFakeHost()): RenderResult {
  return render(
    <HostContext.Provider value={host}>
      {ui}
    </HostContext.Provider>,
  );
}
