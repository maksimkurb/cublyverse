import { render, type RenderResult } from "@testing-library/react";
import { AppRoot } from "@telegram-apps/telegram-ui";
import type { ReactElement } from "react";

import { HostContext } from "../host/HostContext";
import type { HostBridge } from "../host/types";
import { makeFakeHost } from "./fakeHost";

/** Renders a component with the telegram-ui root and a fake host. */
export function renderWithHost(ui: ReactElement, host: HostBridge = makeFakeHost()): RenderResult {
  return render(
    <HostContext.Provider value={host}>
      <AppRoot appearance="light">{ui}</AppRoot>
    </HostContext.Provider>,
  );
}
