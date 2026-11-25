"""Unit tests for the pure part of migrate.py: no network, no database."""

import json
from decimal import Decimal
from pathlib import Path

import pytest

import migrate as m

HERE = Path(__file__).resolve().parent


@pytest.fixture(scope="module")
def dump():
    with open(HERE / "fixture.json", encoding="utf-8") as handle:
        return json.load(handle)


@pytest.fixture(scope="module")
def plan(dump):
    return m.build_plan(dump)


def by_id(plan, grist_id):
    for item in plan.items:
        if item.grist_ids[0] == grist_id:
            return item
    raise AssertionError(f"нет позиции с grist_id {grist_id}")


# --- value helpers ----------------------------------------------------------


@pytest.mark.parametrize(
    ("raw", "expected"),
    [
        ("#D2D057", "#D2D057"),
        ("#d2d057", "#D2D057"),
        ("  #aabbcc  ", "#AABBCC"),
        ("#D2D05", ""),
        ("D2D057", ""),
        ("#D2D0577", ""),
        ("red", ""),
        ("", ""),
        (None, ""),
        (123, ""),
    ],
)
def test_normalize_hex(raw, expected):
    assert m.normalize_hex(raw) == expected


@pytest.mark.parametrize(
    ("raw", "expected"),
    [(None, None), (0, None), ("0", None), ("", None), (7, 7), ("12", 12), (2.0, 2), (2.5, None)],
)
def test_parse_ref(raw, expected):
    assert m.parse_ref(raw) == expected


@pytest.mark.parametrize(
    ("raw", "expected"),
    [
        (None, []),
        ([], []),
        (["L"], []),
        (["L", 3, 1, 3], [3, 1]),
        ([1, 2], [1, 2]),
        (5, [5]),
        (["L", 0, 2, None], [2]),
    ],
)
def test_parse_reflist(raw, expected):
    assert m.parse_reflist(raw) == expected


def test_category_slugs_cover_all_types():
    assert len(m.CATEGORY_SLUGS) == 13
    assert m.category_slug("Мулине") == "floss"
    assert m.category_slug("  шнур / ТРОС ") == "cord"
    assert m.category_slug("Стразы") == "rhinestones"
    assert m.category_slug("Фурнитура") == ""


@pytest.mark.parametrize(
    ("raw", "slug", "expected", "known"),
    [
        ("г", "seed-beads", "г", True),
        ("упаковка", "floss", "упаковка", True),
        ("", "floss", "моток", False),
        ("кг", "floss", "моток", False),
        (None, "rhinestone-chain", "см", False),
        ("", "unknown-slug", "шт", False),
    ],
)
def test_resolve_unit(raw, slug, expected, known):
    assert m.resolve_unit(raw, slug) == (expected, known)


def test_color_source_for():
    assert m.color_source_for("#AABBCC", "") == "approx"
    assert m.color_source_for("#AABBCC", "Approximation") == "approx"
    assert m.color_source_for("", "Approximation") == ""


@pytest.mark.parametrize(
    ("raw", "expected", "problem"),
    [
        (5, Decimal("5.000"), None),
        ("12.5", Decimal("12.500"), None),
        (1.5, Decimal("1.500"), None),
        (None, Decimal("0.000"), None),
        ("", Decimal("0.000"), None),
        ("много", Decimal("0.000"), "не число"),
        (True, Decimal("0.000"), None),
        (-3, Decimal("-3.000"), None),
    ],
)
def test_parse_qty(raw, expected, problem):
    qty, message = m.parse_qty(raw)
    assert qty == expected
    assert (message is not None) == (problem is not None)


# --- plan -------------------------------------------------------------------


def test_plan_manufacturers(plan):
    assert [manufacturer.name for manufacturer in plan.manufacturers] == [
        "DMC",
        "Gamma",
        "PRECIOSA Ornela",
    ]
    dmc = plan.manufacturers[0]
    assert dmc.website == "https://www.dmc.com"
    assert dmc.notes == "Франция"


def test_plan_projects(plan):
    assert [project.name for project in plan.projects] == [
        "Серьги «Роза»",
        "Браслет",
        "Закладка",
    ]


def test_plan_counts(plan):
    # 16 Grist rows - 2 skipped - 2 merged duplicates = 12 items.
    assert len(plan.items) == 12
    assert plan.skipped == 2
    assert plan.duplicates_merged == 2
    assert len(plan.duplicate_reports) == 2


def test_merge_sums_stock_and_projects(plan):
    merged = by_id(plan, 101)
    assert merged.grist_ids == [101, 102]
    assert merged.qty == Decimal("7.000")
    assert merged.project_names == ["Серьги «Роза»", "Браслет", "Закладка"]
    # Empty fields of the first row are filled from the duplicate.
    assert merged.size == "8 м"
    assert merged.attrs()["grist_ids"] == [101, 102]


def test_second_merge_uses_trimmed_article(plan):
    merged = by_id(plan, 107)
    assert merged.grist_ids == [107, 115]
    assert merged.article == "1152"
    assert merged.qty == Decimal("12.000")
    assert merged.project_names == ["Закладка", "Серьги «Роза»"]


def test_same_article_in_other_category_is_not_merged(plan):
    # 110 and 114 share manufacturer PRECIOSA Ornela and article PR-9 but not
    # the category, so they stay apart.
    articles = [item for item in plan.items if item.article == "PR-9"]
    assert len(articles) == 2
    assert {item.category_slug for item in articles} == {"rhinestones", "seed-beads"}


def test_item_mapping(plan):
    item = by_id(plan, 101)
    assert item.name == "Мулине DMC 321"
    assert item.category_slug == "floss"
    assert item.manufacturer == "DMC"
    assert item.article == "321"
    assert item.series == "Mouliné Spécial"
    assert item.color_name == "красный"
    assert item.color_hex == "#C8102E"
    assert item.color_source == "approx"
    assert item.unit == "моток"
    assert item.source_url == "https://www.dmc.com/321"
    assert item.color_rgb == (200, 16, 46)
    attrs = item.attrs()
    assert attrs["grist_id"] == 101
    assert attrs["source_text"] == "старый текст"
    assert attrs["legacy_color_data_source"] == "Representative chart"
    assert attrs["notes"] == "подарок"
    assert (attrs["color_r"], attrs["color_g"], attrs["color_b"]) == (200, 16, 46)


def test_name_fallbacks(plan):
    assert by_id(plan, 107).name == "DMC 1152"
    assert by_id(plan, 113).name == "Материал #113"


def test_unit_fallbacks(plan):
    empty_unit = by_id(plan, 103)
    assert empty_unit.unit == "упаковка"
    assert empty_unit.raw_unit == ""
    assert empty_unit.unit_known is False

    bad_unit = by_id(plan, 112)
    assert bad_unit.unit == "моток"
    assert bad_unit.unit_known is False


def test_malformed_hex_reported(plan):
    assert len(plan.malformed_hex) == 1
    assert "#D2D05" in plan.malformed_hex[0]
    assert by_id(plan, 104).color_hex == ""


def test_unknown_units_reported(plan):
    assert len(plan.unknown_units) == 2
    assert any("«кг»" in entry for entry in plan.unknown_units)
    assert any("не указана" in entry for entry in plan.unknown_units)


def test_unknown_categories_skipped(plan):
    assert len(plan.unknown_categories) == 2
    assert any("Фурнитура" in entry for entry in plan.unknown_categories)
    assert any("Type пуст" in entry for entry in plan.unknown_categories)


def test_negative_and_broken_stock(plan):
    clamped = by_id(plan, 110)
    assert clamped.qty == Decimal("0.000")
    broken = by_id(plan, 116)
    assert broken.qty == Decimal("0.000")
    assert any("отрицательный остаток" in entry for entry in plan.problems)
    assert any("не число" in entry for entry in plan.problems)


def test_unknown_project_reference(plan):
    item = by_id(plan, 111)
    assert item.project_names == []
    assert any("проект #99" in entry for entry in plan.problems)


def test_color_source_empty_without_hex(plan):
    item = by_id(plan, 105)
    assert item.color_hex == ""
    assert item.color_source == ""
    assert item.color_data_source == "Gamma chart 2024"


# --- global conflicts -------------------------------------------------------


def test_compute_conflicts_reports_only_differing_legacy_values(plan):
    item = by_id(plan, 101)
    official = {
        "name": "Официальный DMC 321",
        "color_name": "красный",
        "color_hex": "#C8102E",
        "size": "",
        "finish": "",
        "unit": "моток",
    }
    fields = [conflict[0] for conflict in m.compute_conflicts(item, official)]
    assert fields == ["name"]
    assert m.compute_conflicts(item, official)[0] == (
        "name",
        "Мулине DMC 321",
        "Официальный DMC 321",
    )


def test_compute_conflicts_compares_hex_case_insensitively(plan):
    item = by_id(plan, 101)
    official = {"name": item.name, "color_hex": "#c8102e"}
    assert m.compute_conflicts(item, official) == []


def test_compute_conflicts_always_reports_unit(plan):
    item = by_id(plan, 101)
    official = {"name": item.name, "color_hex": "#C8102E", "unit": "г"}
    conflicts = m.compute_conflicts(item, official)
    assert conflicts == [("unit", "моток", "г")]


# --- report -----------------------------------------------------------------


def test_format_report_dry_run(plan):
    report = m.Report(workspace_name="Тест")
    report.items_created = 3
    report.conflicts = ["конфликт"]
    report.problems = ["проблема"]
    text = m.format_report(plan, report, True, "doc", "00000000-0000-0000-0000-000000000001")
    assert "Рабочее пространство: Тест" in text
    assert "создано 3" in text
    assert "конфликт" in text
    assert "проблема" in text
    assert text.splitlines()[-1].startswith("ИТОГ: сухой прогон")
    assert plan.duplicate_reports[0] in text


def test_format_report_apply(plan):
    text = m.format_report(plan, m.Report(), False, "doc", "ws")
    assert text.splitlines()[-1] == "ИТОГ: изменения применены и зафиксированы."


@pytest.mark.parametrize(
    ("maker", "slug", "article", "size", "series", "want"),
    [
        ("DMC", "floss", "2", "", "", ["2", "02"]),
        ("DMC", "floss", "321", "", "", ["321"]),
        ("PRECIOSA Ornela", "seed-beads", "3254", "10/0", "Rocailles",
         ["3254", "311-19001-10_0-03254"]),
        # Without a size, or for another shape, the product is ambiguous.
        ("PRECIOSA Ornela", "seed-beads", "58310", "", "Rocailles", ["58310"]),
        ("PRECIOSA Ornela", "seed-beads", "38686", "10/0", "3-cuts", ["38686"]),
        ("Gamma", "floss", "5", "", "", ["5"]),
    ],
)
def test_official_articles(maker, slug, article, size, series, want):
    item = m.MaterialItem(
        grist_ids=[1], name="x", category_slug=slug, category_name="x",
        manufacturer=maker, article=article, size=size, series=series,
    )
    assert m.official_articles(item) == want
