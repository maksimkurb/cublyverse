#!/usr/bin/env python3
"""One-off import of a Grist document into the Cublyverse craft catalogue.

The script is deliberately standalone: it lives outside the Go module, uses
only the Python standard library for HTTP and psycopg for the database, and is
run by hand (see README.md next to this file). It must not be imported by the
application.

The target schema is internal/store/migrations/00011_craft.sql. The mapping is
done by pure functions (no network, no database) so it can be tested with
pytest against a small fixture; the database part is a thin writer on top of
the resulting plan.

Usage:
    python3 migrate.py --workspace <uuid> --user <uuid>            # dry run
    python3 migrate.py --workspace <uuid> --user <uuid> --apply    # write
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
import uuid
from dataclasses import dataclass, field
from datetime import datetime, timezone
from decimal import Decimal, InvalidOperation, ROUND_HALF_UP
from typing import Any, Iterable

# The source document is fixed; --doc exists for re-imports from a copy.
DEFAULT_DOC_ID = "q8Q3PUwU5uHSnTD6bQo6xC"

# Grist tables read by the import.
TABLES = ("MaterialTypes", "Manufacturers", "Projects", "Materials")

# Values written into craft_catalog_items.source and craft_inventory_log.reason.
SOURCE_TAG = "grist-import"
LOG_REASON = "импорт из Grist"

# Units accepted by craft_catalog_items.unit (see the check constraint).
UNITS = ("шт", "г", "моток", "м", "см", "упаковка", "набор")

# MaterialTypes.Name -> craft_categories.slug.
CATEGORY_SLUGS = {
    "Мулине": "floss",
    "Бисер": "seed-beads",
    "Стеклярус": "bugles",
    "Рубка": "cut-beads",
    "Жемчуг": "pearls",
    "Бусины": "beads",
    "Пайетки": "sequins",
    "Канитель": "bullion-wire",
    "Стразы": "rhinestones",
    "Оправа": "settings",
    "Стразовая цепь": "rhinestone-chain",
    "Шнур / трос": "cord",
    "Ткань": "fabric",
}

# craft_categories.default_unit, used when Grist has no usable unit.
CATEGORY_DEFAULT_UNITS = {
    "floss": "моток",
    "seed-beads": "упаковка",
    "bugles": "г",
    "cut-beads": "г",
    "pearls": "шт",
    "beads": "г",
    "sequins": "г",
    "bullion-wire": "г",
    "rhinestones": "шт",
    "settings": "шт",
    "rhinestone-chain": "см",
    "cord": "м",
    "fabric": "шт",
}

HEX_RE = re.compile(r"^#[0-9A-Fa-f]{6}$")

# Fields compared when a Grist material is linked to an official global item.
CONFLICT_FIELDS = ("name", "color_name", "color_hex", "size", "finish")


class MigrationError(Exception):
    """A user-facing error: printed without a traceback."""


class DryRun(Exception):
    """Signals the transaction to roll back at the end of a dry run."""


# --- value helpers ----------------------------------------------------------


def as_text(value: Any) -> str:
    """Grist cell -> trimmed text. Missing values become ""."""
    if value is None or isinstance(value, bool):
        return ""
    if isinstance(value, str):
        return value.strip()
    if isinstance(value, float):
        return str(int(value)) if value.is_integer() else repr(value)
    return str(value).strip()


def as_int(value: Any) -> int | None:
    """Grist number/text -> int, or None when it is not one."""
    if value is None or isinstance(value, bool):
        return None
    if isinstance(value, int):
        return value
    if isinstance(value, float):
        return int(value) if value.is_integer() else None
    text = as_text(value)
    if not text:
        return None
    try:
        return int(text)
    except ValueError:
        return None


def parse_ref(value: Any) -> int | None:
    """A Grist Ref cell. 0, null and garbage mean "no reference"."""
    ref = as_int(value)
    if ref is None or ref <= 0:
        return None
    return ref


def parse_reflist(value: Any) -> list[int]:
    """A Grist RefList cell (["L", id, id] or null) -> ordered unique ids."""
    if value is None:
        return []
    if isinstance(value, (list, tuple)):
        items: Iterable[Any] = value
        if items and isinstance(items[0], str):
            # Grist prefixes reference lists with the "L" marker.
            items = list(items)[1:]
    else:
        items = [value]
    out: list[int] = []
    for entry in items:
        ref = parse_ref(entry)
        if ref is not None and ref not in out:
            out.append(ref)
    return out


def normalize_hex(value: Any) -> str:
    """#RRGGBB or "" for anything else. Valid colours are upper-cased."""
    text = as_text(value)
    if HEX_RE.match(text):
        return text.upper()
    return ""


def parse_qty(value: Any) -> tuple[Decimal, str | None]:
    """CurrentStock -> (quantity with three decimals, problem text or None)."""
    if value is None or isinstance(value, bool):
        return Decimal("0.000"), None
    if isinstance(value, str):
        raw = value.strip()
        if not raw:
            return Decimal("0.000"), None
    elif isinstance(value, (int, float)):
        raw = str(value)
    else:
        raw = as_text(value)
        if not raw:
            return Decimal("0.000"), None
    try:
        qty = Decimal(raw)
    except InvalidOperation:
        return Decimal("0.000"), f"«{raw}» — не число"
    if not qty.is_finite():
        return Decimal("0.000"), f"«{raw}» — не число"
    return qty.quantize(Decimal("0.001"), rounding=ROUND_HALF_UP), None


def category_slug(name: Any) -> str:
    """MaterialTypes.Name -> category slug, or "" when it is not mapped."""
    text = " ".join(as_text(name).split())
    for category_name, slug in CATEGORY_SLUGS.items():
        if category_name.casefold() == text.casefold():
            return slug
    return ""


def resolve_unit(unit: Any, slug: str) -> tuple[str, bool]:
    """Grist unit -> (valid unit, True) or (category default, False)."""
    text = as_text(unit)
    if text in UNITS:
        return text, True
    return CATEGORY_DEFAULT_UNITS.get(slug, "шт"), False


def color_source_for(color_hex: str, data_source: Any) -> str:
    """Colour trust for an imported hex.

    Only "official" and "approx" exist, and we can never prove a Grist hex is
    official, so any imported colour is "approx". The free-text origin is kept
    in attrs.legacy_color_data_source for a human to review.
    """
    _ = data_source  # kept for the caller's intent and for future heuristics
    return "approx" if color_hex else ""


def truncate(text: str, limit: int, problems: list[str], label: str) -> str:
    if len(text) <= limit:
        return text
    problems.append(f"{label}: текст длиннее {limit} символов — обрезан.")
    return text[:limit]


# --- plan -------------------------------------------------------------------


@dataclass
class ManufacturerRecord:
    name: str
    website: str = ""
    notes: str = ""


@dataclass
class ProjectRecord:
    name: str
    notes: str = ""


@dataclass
class MaterialItem:
    """One catalogue item to create, already merged with its Grist duplicates."""

    grist_ids: list[int]
    name: str
    category_slug: str
    category_name: str
    manufacturer: str = ""
    article: str = ""
    series: str = ""
    color_name: str = ""
    color_hex: str = ""
    color_source: str = ""
    size: str = ""
    finish: str = ""
    unit: str = ""
    source_url: str = ""
    notes: str = ""
    source_text: str = ""
    color_data_source: str = ""
    color_rgb: tuple[int, int, int] | None = None
    qty: Decimal = field(default_factory=lambda: Decimal("0.000"))
    project_names: list[str] = field(default_factory=list)
    # Raw Grist values and warnings, used only to build the report.
    raw_hex: str = ""
    raw_unit: str = ""
    unit_known: bool = True
    warnings: list[str] = field(default_factory=list)

    @property
    def label(self) -> str:
        return f"#{self.grist_ids[0]} «{self.name}»"

    def attrs(self) -> dict[str, Any]:
        attrs: dict[str, Any] = {
            "grist_id": self.grist_ids[0],
            "source_text": self.source_text,
            "legacy_color_data_source": self.color_data_source,
            "notes": self.notes,
        }
        if len(self.grist_ids) > 1:
            attrs["grist_ids"] = self.grist_ids
        if self.color_rgb is not None:
            attrs["color_r"], attrs["color_g"], attrs["color_b"] = self.color_rgb
        return attrs


@dataclass
class ImportPlan:
    manufacturers: list[ManufacturerRecord] = field(default_factory=list)
    items: list[MaterialItem] = field(default_factory=list)
    projects: list[ProjectRecord] = field(default_factory=list)
    # Report sections requested by the migration brief.
    unknown_categories: list[str] = field(default_factory=list)
    unknown_units: list[str] = field(default_factory=list)
    malformed_hex: list[str] = field(default_factory=list)
    problems: list[str] = field(default_factory=list)
    duplicates_merged: int = 0
    duplicate_reports: list[str] = field(default_factory=list)
    skipped: int = 0


def table_records(dump: dict[str, Any], table: str) -> list[dict[str, Any]]:
    tables = dump.get("tables")
    if isinstance(tables, dict):
        rows = tables.get(table)
    else:
        rows = dump.get(table)
    if rows is None:
        return []
    if not isinstance(rows, list):
        raise MigrationError(f"дамп: таблица {table} должна быть списком записей")
    return rows


def record_id(record: dict[str, Any]) -> int | None:
    return as_int(record.get("id"))


def record_fields(record: dict[str, Any]) -> dict[str, Any]:
    fields = record.get("fields")
    if isinstance(fields, dict):
        return fields
    return record


def build_plan(dump: dict[str, Any]) -> ImportPlan:
    """Turn a fetched (or saved) Grist dump into a database-independent plan."""
    plan = ImportPlan()

    type_names: dict[int, str] = {}
    for record in table_records(dump, "MaterialTypes"):
        rid = record_id(record)
        name = as_text(record_fields(record).get("Name"))
        if rid is not None and name:
            type_names[rid] = name

    manufacturers_by_id: dict[int, str] = {}
    seen_manufacturers: dict[str, ManufacturerRecord] = {}
    for record in table_records(dump, "Manufacturers"):
        rid = record_id(record)
        fields = record_fields(record)
        name = as_text(fields.get("Name"))
        if not name:
            plan.problems.append(f"Производитель #{rid}: пустое название — пропущен.")
            continue
        name = truncate(name, 120, plan.problems, f"Производитель #{rid}")
        if rid is not None:
            manufacturers_by_id[rid] = name
        website = as_text(fields.get("Website"))
        notes = as_text(fields.get("Notes"))
        existing = seen_manufacturers.get(name)
        if existing is None:
            seen_manufacturers[name] = ManufacturerRecord(name=name, website=website, notes=notes)
        else:
            # Two Grist rows with the same name: keep the first non-empty values.
            existing.website = existing.website or website
            existing.notes = existing.notes or notes
    plan.manufacturers = list(seen_manufacturers.values())

    project_names_by_id: dict[int, str] = {}
    seen_projects: dict[str, ProjectRecord] = {}
    for record in table_records(dump, "Projects"):
        rid = record_id(record)
        fields = record_fields(record)
        name = as_text(fields.get("Name"))
        if not name:
            plan.problems.append(f"Проект #{rid}: пустое название — пропущен.")
            continue
        name = truncate(name, 200, plan.problems, f"Проект #{rid}")
        if rid is not None:
            project_names_by_id[rid] = name
        notes = as_text(fields.get("Notes"))
        existing = seen_projects.get(name)
        if existing is None:
            seen_projects[name] = ProjectRecord(name=name, notes=notes)
        else:
            existing.notes = existing.notes or notes
    plan.projects = list(seen_projects.values())

    mapped: list[MaterialItem] = []
    for record in table_records(dump, "Materials"):
        item, problem = map_material(record, type_names, manufacturers_by_id, project_names_by_id)
        if item is None:
            plan.skipped += 1
            plan.unknown_categories.append(problem or f"#{record_id(record)}: категория не определена")
            continue
        # Warnings are collected before merging so a dropped duplicate still
        # shows up in the report.
        _collect_item_warnings(item, plan)
        mapped.append(item)

    merged, duplicates, duplicate_reports = merge_duplicates(mapped)
    plan.items = merged
    plan.duplicates_merged = duplicates
    plan.duplicate_reports = duplicate_reports

    return plan


def map_material(
    record: dict[str, Any],
    type_names: dict[int, str],
    manufacturers_by_id: dict[int, str],
    project_names_by_id: dict[int, str],
) -> tuple[MaterialItem | None, str | None]:
    """Map one Materials row. Returns (item, None) or (None, skip reason)."""
    rid = record_id(record)
    if rid is None:
        return None, "у записи нет id"
    fields = record_fields(record)

    type_ref = parse_ref(fields.get("Type"))
    type_name = type_names.get(type_ref, "") if type_ref is not None else ""
    slug = category_slug(type_name)
    if not slug:
        if type_ref is None:
            return None, f"#{rid}: не указан тип материала (Type пуст)"
        return None, f"#{rid}: тип #{type_ref} «{type_name}» не сопоставлен категории"

    manufacturer_ref = parse_ref(fields.get("Manufacturer"))
    manufacturer = ""
    if manufacturer_ref is not None:
        manufacturer = manufacturers_by_id.get(manufacturer_ref, "")

    warnings: list[str] = []
    article = as_text(fields.get("Article"))
    name = as_text(fields.get("Name"))
    if not name:
        name = " ".join(part for part in (manufacturer, article) if part)
    if not name:
        name = f"Материал #{rid}"
    name = truncate(name, 200, warnings, f"#{rid}")

    raw_hex = as_text(fields.get("ColorHex"))
    color_hex = normalize_hex(raw_hex)
    data_source = as_text(fields.get("ColorDataSource"))

    color_rgb: tuple[int, int, int] | None = None
    channels = [as_int(fields.get(channel)) for channel in ("ColorR", "ColorG", "ColorB")]
    if all(channel is not None and 0 <= channel <= 255 for channel in channels):
        red, green, blue = channels
        color_rgb = (int(red), int(green), int(blue))

    raw_unit = as_text(fields.get("Unit"))
    unit, unit_known = resolve_unit(raw_unit, slug)
    qty, qty_problem = parse_qty(fields.get("CurrentStock"))
    if qty_problem is not None:
        warnings.append(f"#{rid}: остаток {qty_problem} — принят 0.")
    elif qty < 0:
        warnings.append(f"#{rid}: отрицательный остаток {qty} — принят 0.")
        qty = Decimal("0.000")

    project_names: list[str] = []
    for project_ref in parse_reflist(fields.get("UsedIn")):
        project_name = project_names_by_id.get(project_ref)
        if project_name is None:
            warnings.append(f"#{rid}: проект #{project_ref} не найден — связь пропущена.")
            continue
        if project_name not in project_names:
            project_names.append(project_name)

    item = MaterialItem(
        grist_ids=[rid],
        name=name,
        category_slug=slug,
        category_name=" ".join(type_name.split()),
        manufacturer=manufacturer,
        article=article,
        series=as_text(fields.get("Series")),
        color_name=as_text(fields.get("Color")),
        color_hex=color_hex,
        color_source=color_source_for(color_hex, data_source),
        size=as_text(fields.get("Size")),
        finish=as_text(fields.get("Finish")),
        unit=unit,
        source_url=as_text(fields.get("ManufacturerURL")),
        notes=as_text(fields.get("Notes")),
        source_text=as_text(fields.get("SourceText")),
        color_data_source=data_source,
        color_rgb=color_rgb,
        qty=qty,
        project_names=project_names,
        raw_hex=raw_hex,
        raw_unit=raw_unit,
        unit_known=unit_known,
        warnings=warnings,
    )
    return item, None


def _collect_item_warnings(item: MaterialItem, plan: ImportPlan) -> None:
    if item.raw_hex and not item.color_hex:
        plan.malformed_hex.append(
            f"{item.label}: ColorHex «{item.raw_hex}» не похож на #RRGGBB — цвет не перенесён."
        )
    if not item.unit_known:
        if item.raw_unit:
            plan.unknown_units.append(
                f"{item.label}: единица «{item.raw_unit}» не распознана — используется «{item.unit}»."
            )
        else:
            plan.unknown_units.append(
                f"{item.label}: единица не указана — используется «{item.unit}»."
            )
    plan.problems.extend(item.warnings)


def merge_duplicates(items: list[MaterialItem]) -> tuple[list[MaterialItem], int, list[str]]:
    """Fold rows that would collide on (workspace, manufacturer, category, article).

    The database unique index ignores case on nothing (it compares text), so the
    key uses the exact article. Manufacturer names are case-folded because they
    resolve to the same manufacturer row.
    """
    merged: list[MaterialItem] = []
    by_key: dict[tuple[str, str, str], MaterialItem] = {}
    duplicates = 0
    reports: list[str] = []
    for item in items:
        key = merge_key(item)
        target = by_key.get(key) if key is not None else None
        if target is None:
            merged.append(item)
            if key is not None:
                by_key[key] = item
            continue
        target.grist_ids.extend(item.grist_ids)
        target.qty = (target.qty + item.qty).quantize(Decimal("0.001"), rounding=ROUND_HALF_UP)
        for name in item.project_names:
            if name not in target.project_names:
                target.project_names.append(name)
        _fill_missing(target, item)
        duplicates += 1
        reports.append(
            f"{target.label}: дубль #{item.grist_ids[0]} с тем же артикулом «{target.article}» — "
            f"остатки суммированы ({target.qty})."
        )
    return merged, duplicates, reports


def merge_key(item: MaterialItem) -> tuple[str, str, str] | None:
    if not item.article or not item.category_slug:
        return None
    return (item.category_slug, item.manufacturer.casefold(), item.article)


def _fill_missing(target: MaterialItem, source: MaterialItem) -> None:
    for prop in (
        "color_name",
        "color_hex",
        "size",
        "finish",
        "series",
        "source_url",
        "notes",
        "source_text",
        "color_data_source",
    ):
        if not getattr(target, prop) and getattr(source, prop):
            setattr(target, prop, getattr(source, prop))
    if target.color_rgb is None and source.color_rgb is not None:
        target.color_rgb = source.color_rgb
    target.color_source = color_source_for(target.color_hex, target.color_data_source)


# --- global catalogue conflicts --------------------------------------------


def compute_conflicts(item: MaterialItem, official: dict[str, Any]) -> list[tuple[str, str, str]]:
    """Legacy values that differ from an existing global item.

    Only genuine disagreements are reported: both sides must have a value. A
    legacy value the official item leaves empty loses nothing, and an empty
    legacy value never overrides anything. The unit is always compared when
    both are known because stock quantities mean different things in different
    units.
    """
    conflicts: list[tuple[str, str, str]] = []
    for name in CONFLICT_FIELDS:
        legacy = getattr(item, name)
        current = as_text(official.get(name))
        if name == "color_hex":
            legacy = normalize_hex(legacy)
            current = normalize_hex(current)
        if not legacy or not current or legacy == current:
            continue
        conflicts.append((name, legacy, current))
    official_unit = as_text(official.get("unit"))
    if official_unit and item.unit and official_unit != item.unit:
        conflicts.append(("unit", item.unit, official_unit))
    return conflicts


def conflict_message(
    item: MaterialItem, official: dict[str, Any], field_name: str, legacy: str, current: str
) -> str:
    return (
        f"{item.label} → глобальная «{as_text(official.get('name'))}»: "
        f"{field_name} «{legacy}» / официально «{current}»."
    )


# --- report -----------------------------------------------------------------


@dataclass
class Report:
    workspace_name: str = ""
    manufacturers_created: int = 0
    manufacturers_updated: int = 0
    items_created: int = 0
    items_existing: int = 0
    items_linked_global: int = 0
    items_reused: int = 0
    stock_created: int = 0
    stock_zero: int = 0
    stock_skipped: int = 0
    log_created: int = 0
    projects_created: int = 0
    projects_existing: int = 0
    links_created: int = 0
    links_skipped: int = 0
    conflicts: list[str] = field(default_factory=list)
    problems: list[str] = field(default_factory=list)


def _section(lines: list[str], title: str, entries: list[str]) -> None:
    if not entries:
        return
    lines.append("")
    lines.append(title)
    for entry in entries:
        lines.append(f"  * {entry}")


def format_report(
    plan: ImportPlan, report: Report, dry_run: bool, doc_id: str, workspace_id: str
) -> str:
    lines = [
        f"=== Импорт Grist → Cublyverse: документ {doc_id} ===",
        f"Рабочее пространство: {report.workspace_name or '—'} ({workspace_id})",
        "",
        f"Производители:          создано {report.manufacturers_created}, "
        f"обновлено {report.manufacturers_updated}",
        f"Позиции каталога:       создано {report.items_created}, "
        f"уже импортировано {report.items_existing}, "
        f"привязано к глобальному {report.items_linked_global}, "
        f"использована существующая {report.items_reused}",
        f"Пропущено позиций:      {plan.skipped}",
        f"Дубли в Grist:          объединено {plan.duplicates_merged}",
        f"Остатки:                создано {report.stock_created} "
        f"(из них нулевых {report.stock_zero}), уже было {report.stock_skipped}",
        f"Журнал остатков:        записей {report.log_created}",
        f"Проекты:                создано {report.projects_created}, "
        f"уже было {report.projects_existing}",
        f"Связи проект–материал:  добавлено {report.links_created}, "
        f"уже было {report.links_skipped}",
    ]

    _section(
        lines,
        "Конфликты с глобальным каталогом (официальные данные не перезаписаны):",
        report.conflicts,
    )
    _section(lines, "Объединённые дубли в Grist:", plan.duplicate_reports)
    _section(
        lines,
        "Позиции с неизвестной категорией (пропущены):",
        plan.unknown_categories,
    )
    _section(
        lines,
        "Позиции с нераспознанной единицей измерения:",
        plan.unknown_units,
    )
    _section(lines, "Некорректный ColorHex (цвет не перенесён):", plan.malformed_hex)
    _section(lines, "Прочие предупреждения:", report.problems)

    lines.append("")
    if dry_run:
        lines.append(
            "ИТОГ: сухой прогон — все изменения откачены, база не изменена. "
            "Для записи запустите с --apply."
        )
    else:
        lines.append("ИТОГ: изменения применены и зафиксированы.")
    return "\n".join(lines)


# --- Grist HTTP (stdlib only) ----------------------------------------------


def fetch_table(base_url: str, doc_id: str, api_key: str, table: str, timeout: float = 60.0):
    url = "{}/api/docs/{}/tables/{}/records".format(
        base_url.rstrip("/"),
        urllib.parse.quote(doc_id, safe=""),
        urllib.parse.quote(table, safe=""),
    )
    request = urllib.request.Request(
        url,
        headers={"Authorization": f"Bearer {api_key}", "Accept": "application/json"},
        method="GET",
    )
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            payload = json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        # Never include headers: the API key lives there.
        raise MigrationError(
            f"Grist ответил HTTP {exc.code} на запрос таблицы {table}"
        ) from None
    except urllib.error.URLError as exc:
        raise MigrationError(f"не удалось соединиться с Grist: {exc.reason}") from None
    except (UnicodeDecodeError, json.JSONDecodeError):
        raise MigrationError(f"Grist вернул не JSON для таблицы {table}") from None
    records = payload.get("records") if isinstance(payload, dict) else None
    if not isinstance(records, list):
        raise MigrationError(f"Grist вернул неожиданный ответ для таблицы {table}")
    return records


def fetch_dump(base_url: str, doc_id: str, api_key: str) -> dict[str, Any]:
    tables = {table: fetch_table(base_url, doc_id, api_key, table) for table in TABLES}
    return {
        "doc_id": doc_id,
        "fetched_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "tables": tables,
    }


def load_dump(path: str) -> dict[str, Any]:
    try:
        with open(path, "r", encoding="utf-8") as handle:
            dump = json.load(handle)
    except OSError as exc:
        raise MigrationError(f"не удалось прочитать {path}: {exc.strerror}") from None
    except json.JSONDecodeError as exc:
        raise MigrationError(f"{path}: некорректный JSON ({exc.msg})") from None
    if not isinstance(dump, dict):
        raise MigrationError(f"{path}: ожидался объект JSON")
    return dump


def save_dump(path: str, dump: dict[str, Any]) -> None:
    try:
        with open(path, "w", encoding="utf-8") as handle:
            json.dump(dump, handle, ensure_ascii=False, indent=1)
            handle.write("\n")
    except OSError as exc:
        raise MigrationError(f"не удалось записать {path}: {exc.strerror}") from None


# --- database ---------------------------------------------------------------

SELECT_CATEGORIES = "SELECT slug, id FROM craft_categories"

UPSERT_MANUFACTURER = """
INSERT INTO craft_manufacturers (name, website, notes)
VALUES (%s, %s, %s)
ON CONFLICT (name) DO UPDATE SET
    website = CASE WHEN EXCLUDED.website <> '' THEN EXCLUDED.website
                   ELSE craft_manufacturers.website END,
    notes   = CASE WHEN EXCLUDED.notes <> '' THEN EXCLUDED.notes
                   ELSE craft_manufacturers.notes END
RETURNING id, (xmax = 0) AS inserted
"""

FIND_BY_GRIST_ID = """
SELECT id
FROM craft_catalog_items
WHERE workspace_id = %s::uuid
  AND (attrs->>'grist_id' = %s OR attrs->'grist_ids' @> %s::jsonb)
ORDER BY created_at
LIMIT 1
"""

FIND_GLOBAL_ITEM = """
SELECT i.id, i.name, i.color_name, i.color_hex, i.size, i.finish, i.unit
FROM craft_catalog_items i
JOIN craft_manufacturers m ON m.id = i.manufacturer_id
WHERE i.workspace_id IS NULL
  AND i.category_id = %s::uuid
  AND lower(m.name) = lower(%s)
  AND lower(i.article) = lower(%s)
ORDER BY i.created_at
LIMIT 1
"""

FIND_PRIVATE_BY_ARTICLE = """
SELECT id
FROM craft_catalog_items
WHERE workspace_id = %s::uuid
  AND category_id = %s::uuid
  AND COALESCE(manufacturer_id, '00000000-0000-0000-0000-000000000000'::uuid)
      = COALESCE(%s::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
  AND article = %s
ORDER BY created_at
LIMIT 1
"""

INSERT_ITEM = """
INSERT INTO craft_catalog_items (
    workspace_id, category_id, manufacturer_id, article, series, name,
    color_name, color_hex, color_source, size, finish, unit, attrs, source, source_url
) VALUES (
    %s::uuid, %s::uuid, %s::uuid, %s, %s, %s,
    %s, %s, %s, %s, %s, %s, %s::jsonb, %s, %s
)
RETURNING id
"""

INSERT_INVENTORY = """
INSERT INTO craft_inventory (workspace_id, catalog_item_id, qty, notes)
VALUES (%s::uuid, %s::uuid, %s, %s)
ON CONFLICT (workspace_id, catalog_item_id) DO NOTHING
RETURNING 1
"""

INSERT_INVENTORY_LOG = """
INSERT INTO craft_inventory_log (
    workspace_id, catalog_item_id, user_id, delta, qty_after, reason
) VALUES (%s::uuid, %s::uuid, %s::uuid, %s, %s, %s)
"""

FIND_PROJECT = """
SELECT id
FROM craft_projects
WHERE workspace_id = %s::uuid AND name = %s
ORDER BY created_at, id
LIMIT 1
"""

INSERT_PROJECT = """
INSERT INTO craft_projects (workspace_id, name, notes)
VALUES (%s::uuid, %s, %s)
RETURNING id
"""

INSERT_PROJECT_MATERIAL = """
INSERT INTO craft_project_materials (project_id, catalog_item_id)
VALUES (%s::uuid, %s::uuid)
ON CONFLICT DO NOTHING
RETURNING 1
"""


def connect(dsn: str):
    try:
        import psycopg
    except ImportError:  # pragma: no cover - depends on the environment
        raise MigrationError(
            "не установлен psycopg: выполните `pip install -r requirements.txt`"
        ) from None
    try:
        return psycopg.connect(dsn)
    except psycopg.OperationalError as exc:
        raise MigrationError(f"не удалось подключиться к PostgreSQL: {exc}") from None


def ensure_target(conn, workspace_id: str, user_id: str, report: Report) -> None:
    with conn.cursor() as cur:
        cur.execute("SELECT name FROM workspaces WHERE id = %s::uuid", (workspace_id,))
        row = cur.fetchone()
        if row is None:
            raise MigrationError(
                f"рабочее пространство {workspace_id} не найдено — проверьте --workspace"
            )
        report.workspace_name = row[0]
        cur.execute("SELECT 1 FROM users WHERE id = %s::uuid", (user_id,))
        if cur.fetchone() is None:
            raise MigrationError(f"пользователь {user_id} не найден — проверьте --user")


def apply_plan(conn, plan: ImportPlan, workspace_id: str, user_id: str, report: Report) -> None:
    with conn.cursor() as cur:
        categories = {slug: category_id for slug, category_id in cur.execute(SELECT_CATEGORIES)}
        missing = sorted({item.category_slug for item in plan.items} - set(categories))
        if missing:
            raise MigrationError(
                "в базе нет категорий: "
                + ", ".join(missing)
                + " — примените миграции (make migrate)"
            )

        manufacturer_ids: dict[str, str] = {}
        for manufacturer in plan.manufacturers:
            cur.execute(
                UPSERT_MANUFACTURER,
                (manufacturer.name, manufacturer.website, manufacturer.notes),
            )
            manufacturer_id, inserted = cur.fetchone()
            manufacturer_ids[manufacturer.name] = manufacturer_id
            if inserted:
                report.manufacturers_created += 1
            else:
                report.manufacturers_updated += 1

        imported: list[tuple[MaterialItem, str]] = []
        for item in plan.items:
            category_id = categories[item.category_slug]
            manufacturer_id = (
                manufacturer_ids.get(item.manufacturer) if item.manufacturer else None
            )

            target_id = find_by_grist_id(cur, workspace_id, item)
            if target_id is not None:
                report.items_existing += 1
            else:
                official = None
                if manufacturer_id and item.article:
                    official = find_global_item(
                        cur, category_id, item.manufacturer, item.article
                    )
                if official is not None:
                    target_id = official["id"]
                    report.items_linked_global += 1
                    for field_name, legacy, current in compute_conflicts(item, official):
                        report.conflicts.append(
                            conflict_message(item, official, field_name, legacy, current)
                        )
                else:
                    target_id = find_private_by_article(
                        cur, workspace_id, category_id, manufacturer_id, item.article
                    )
                    if target_id is not None:
                        report.items_reused += 1
                        report.problems.append(
                            f"{item.label}: позиция с таким производителем и артикулом "
                            "уже есть в рабочем пространстве — используется она."
                        )
                    else:
                        cur.execute(
                            INSERT_ITEM,
                            (
                                workspace_id,
                                category_id,
                                manufacturer_id,
                                item.article,
                                item.series,
                                item.name,
                                item.color_name,
                                item.color_hex,
                                item.color_source,
                                item.size,
                                item.finish,
                                item.unit,
                                json.dumps(item.attrs(), ensure_ascii=False),
                                SOURCE_TAG,
                                item.source_url,
                            ),
                        )
                        target_id = cur.fetchone()[0]
                        report.items_created += 1

            imported.append((item, target_id))
            import_stock(cur, workspace_id, user_id, item, target_id, report)

        project_ids: dict[str, str] = {}
        for project in plan.projects:
            cur.execute(FIND_PROJECT, (workspace_id, project.name))
            row = cur.fetchone()
            if row is not None:
                project_ids[project.name] = row[0]
                report.projects_existing += 1
                continue
            cur.execute(INSERT_PROJECT, (workspace_id, project.name, project.notes))
            project_ids[project.name] = cur.fetchone()[0]
            report.projects_created += 1

        for item, catalog_item_id in imported:
            for project_name in item.project_names:
                project_id = project_ids.get(project_name)
                if project_id is None:
                    continue
                cur.execute(INSERT_PROJECT_MATERIAL, (project_id, catalog_item_id))
                if cur.fetchone() is not None:
                    report.links_created += 1
                else:
                    report.links_skipped += 1


def find_by_grist_id(cur, workspace_id: str, item: MaterialItem) -> str | None:
    cur.execute(
        FIND_BY_GRIST_ID,
        (workspace_id, str(item.grist_ids[0]), json.dumps(item.grist_ids)),
    )
    row = cur.fetchone()
    return row[0] if row is not None else None


def find_global_item(
    cur, category_id: str, manufacturer: str, article: str
) -> dict[str, Any] | None:
    cur.execute(FIND_GLOBAL_ITEM, (category_id, manufacturer, article))
    row = cur.fetchone()
    if row is None:
        return None
    keys = ("id", "name", "color_name", "color_hex", "size", "finish", "unit")
    return dict(zip(keys, row))


def find_private_by_article(
    cur, workspace_id: str, category_id: str, manufacturer_id: str | None, article: str
) -> str | None:
    if not article:
        return None
    cur.execute(
        FIND_PRIVATE_BY_ARTICLE,
        (workspace_id, category_id, manufacturer_id, article),
    )
    row = cur.fetchone()
    return row[0] if row is not None else None


def import_stock(
    cur,
    workspace_id: str,
    user_id: str,
    item: MaterialItem,
    catalog_item_id: str,
    report: Report,
) -> None:
    """Write the stock row once and log the delta only when the row is new."""
    cur.execute(INSERT_INVENTORY, (workspace_id, catalog_item_id, item.qty, item.notes))
    if cur.fetchone() is None:
        report.stock_skipped += 1
        return
    report.stock_created += 1
    if item.qty == 0:
        report.stock_zero += 1
        return
    cur.execute(
        INSERT_INVENTORY_LOG,
        (workspace_id, catalog_item_id, user_id, item.qty, item.qty, LOG_REASON),
    )
    report.log_created += 1


# --- CLI --------------------------------------------------------------------


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Импорт документа Grist в каталог «Бусинки» Cublyverse.",
    )
    parser.add_argument("--workspace", required=True, help="uuid рабочего пространства")
    parser.add_argument(
        "--user", required=True, help="uuid пользователя для журнала остатков"
    )
    parser.add_argument(
        "--dsn", default=None, help="DSN PostgreSQL (по умолчанию DATABASE_URL)"
    )
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument(
        "--apply", action="store_true", help="записать изменения в базу"
    )
    mode.add_argument(
        "--dry-run",
        action="store_true",
        help="только отчёт, изменения откатываются (по умолчанию)",
    )
    parser.add_argument(
        "--from-json",
        metavar="FILE",
        help="читать ранее сохранённый дамп вместо обращения к Grist",
    )
    parser.add_argument(
        "--save-json", metavar="FILE", help="сохранить полученные из Grist данные"
    )
    parser.add_argument(
        "--doc", default=DEFAULT_DOC_ID, help=f"id документа Grist (по умолчанию {DEFAULT_DOC_ID})"
    )
    parser.add_argument(
        "--grist-url",
        default=None,
        help="базовый адрес Grist (по умолчанию GRIST_URL)",
    )
    return parser.parse_args(argv)


def normalize_uuid(value: str, flag: str) -> str:
    try:
        return str(uuid.UUID(value.strip()))
    except (ValueError, AttributeError):
        raise MigrationError(f"{flag}: «{value}» не похож на uuid") from None


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    try:
        dsn = args.dsn or os.environ.get("DATABASE_URL", "")
        if not dsn.strip():
            raise MigrationError("укажите --dsn или переменную окружения DATABASE_URL")
        workspace_id = normalize_uuid(args.workspace, "--workspace")
        user_id = normalize_uuid(args.user, "--user")
        if args.from_json and args.save_json:
            raise MigrationError("--from-json и --save-json взаимоисключающие")

        if args.from_json:
            dump = load_dump(args.from_json)
        else:
            grist_url = args.grist_url or os.environ.get("GRIST_URL", "")
            api_key = os.environ.get("GRIST_API_KEY", "")
            if not grist_url.strip():
                raise MigrationError(
                    "укажите --grist-url или переменную окружения GRIST_URL"
                )
            if not grist_url.startswith(("http://", "https://")):
                raise MigrationError(
                    "GRIST_URL должен начинаться с http:// или https://"
                )
            if not api_key.strip():
                raise MigrationError("переменная окружения GRIST_API_KEY не задана")
            dump = fetch_dump(grist_url, args.doc, api_key)
            if args.save_json:
                save_dump(args.save_json, dump)

        plan = build_plan(dump)
        dry_run = not args.apply
        report = Report()
        report.problems.extend(plan.problems)

        conn = connect(dsn)
        try:
            try:
                with conn.transaction():
                    ensure_target(conn, workspace_id, user_id, report)
                    apply_plan(conn, plan, workspace_id, user_id, report)
                    if dry_run:
                        raise DryRun()
            except DryRun:
                pass
        finally:
            conn.close()

        print(format_report(plan, report, dry_run, args.doc, workspace_id))
        return 0
    except MigrationError as exc:
        print(f"ошибка: {exc}", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        print("прервано", file=sys.stderr)
        return 130


if __name__ == "__main__":
    sys.exit(main())
