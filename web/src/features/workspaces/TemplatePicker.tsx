import { Segmented } from "../../ui/controls";

/** Starting content of a new warehouse. */
export type WarehouseTemplate = "craft" | "empty";

const TEMPLATES: ReadonlyArray<{ value: WarehouseTemplate; label: string }> = [
  { value: "craft", label: "Рукоделие" },
  { value: "empty", label: "Пустой" },
];

const HINTS: Record<WarehouseTemplate, string> = {
  craft: "Категории и единицы для рукоделия, каталоги DMC и PRECIOSA.",
  empty: "Только единица «шт»: категории и единицы заведёте сами.",
};

/** Choice of the template of a new warehouse. */
export function TemplatePicker({ value, onChange }: { value: WarehouseTemplate; onChange: (value: WarehouseTemplate) => void }) {
  return (
    <div className="cv-form">
      <Segmented label="Шаблон склада" value={value} options={TEMPLATES} onChange={onChange} />
      <p className="page__state">{HINTS[value]}</p>
    </div>
  );
}
