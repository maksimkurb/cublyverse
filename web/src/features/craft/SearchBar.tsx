import { useState, type ReactNode } from "react";

import { TextField } from "../../ui/controls";
import { IconClose, IconPipette } from "../../ui/icons";
import { hexFromQuery } from "./color";
import { ColorPickSheet } from "./ColorPickSheet";

/** What a list is searched by: text, a colour, or both. */
export function useCraftSearch() {
  const [text, setText] = useState("");
  const [picked, setPicked] = useState("");
  const typed = hexFromQuery(text);
  return {
    text,
    setText,
    setPicked,
    /** The text to search for; empty when the field holds a HEX. */
    q: typed ? "" : text,
    /** The colour to search by; empty for none. */
    color: typed ?? picked,
    clearColor: () => {
      setPicked("");
      if (typed) {
        setText("");
      }
    },
  };
}

export type CraftSearch = ReturnType<typeof useCraftSearch>;

/** The search field with the colour button and an optional main action. */
export function SearchBar({ search, label = "Поиск", children }: { search: CraftSearch; label?: string; children?: ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="craft-search">
      <div className="craft-toolbar">
        <TextField label={label} placeholder="Название, артикул, цвет" value={search.text} onChange={(event) => search.setText(event.target.value)} />
        <button type="button" className="cv-iconbtn craft-search__color" aria-label="Поиск по цвету" title="Поиск по цвету" onClick={() => setOpen(true)}>
          <IconPipette />
        </button>
        {children}
      </div>
      {search.color && (
        <div className="craft-search__active">
          <span className="craft-colordot" style={{ background: search.color }} aria-hidden="true" />
          <span>
            Похожие на <span className="craft-search__hex">{search.color}</span>: сначала ближайшие, позиции без цвета скрыты
          </span>
          <button type="button" className="cv-iconbtn" aria-label="Убрать поиск по цвету" onClick={search.clearColor}>
            <IconClose />
          </button>
        </div>
      )}
      {open && (
        <ColorPickSheet
          initial={search.color}
          action="Найти похожие"
          onPick={(hex) => {
            // A typed HEX would otherwise win over the picked colour.
            if (hexFromQuery(search.text)) {
              search.setText("");
            }
            search.setPicked(hex);
            setOpen(false);
          }}
          onClose={() => setOpen(false)}
        />
      )}
    </div>
  );
}
