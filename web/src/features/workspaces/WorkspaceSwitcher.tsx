import type { Workspace } from "../../api/client";
import { SelectField } from "../../ui/controls";

/** Props of the shared workspace switcher. */
export interface WorkspaceSwitcherProps {
  workspaces: Workspace[];
  currentId: string | null;
  onChange: (id: string) => void;
}

/** The picker shown at the top of every workspace screen. */
export function WorkspaceSwitcher({ workspaces, currentId, onChange }: WorkspaceSwitcherProps) {
  if (workspaces.length === 0) {
    return null;
  }
  return (
    <div className="ws-switcher">
      <SelectField label="Пространство" value={currentId ?? workspaces[0].id} onChange={(event) => onChange(event.target.value)}>
        {workspaces.map((workspace) => (
          <option key={workspace.id} value={workspace.id}>
            {workspace.name}
          </option>
        ))}
      </SelectField>
    </div>
  );
}
