"""Optional end-to-end test: a real PostgreSQL 17 in Docker.

The unit tests cover the mapping; this one covers the SQL. It is skipped unless
GRIST_TEST_INTEGRATION=1 is set and docker is available:

    GRIST_TEST_INTEGRATION=1 python -m pytest -m integration

It applies the Up sections of all migrations, seeds a user, a
warehouse and one official global item, then runs migrate.py twice and asserts
that the second run changes nothing.
"""

import json
import os
import shutil
import subprocess
import sys
import time
import uuid
from pathlib import Path

import pytest

HERE = Path(__file__).resolve().parent
SCRIPT = HERE.parent / "migrate.py"
FIXTURE = HERE / "fixture.json"
REPO_ROOT = HERE.parents[2]

pytestmark = [
    pytest.mark.integration,
    pytest.mark.skipif(
        os.environ.get("GRIST_TEST_INTEGRATION") != "1",
        reason="set GRIST_TEST_INTEGRATION=1 to run the Docker test",
    ),
    pytest.mark.skipif(shutil.which("docker") is None, reason="docker is not installed"),
]

PG_IMAGE = "postgres:17"


def goose_up(path: Path) -> str:
    text = path.read_text(encoding="utf-8")
    start = text.index("-- +goose Up")
    end = text.index("-- +goose Down")
    lines = text[start:end].splitlines()
    return "\n".join(line for line in lines if not line.strip().startswith("-- +goose"))


def docker_port(container: str, internal: str) -> str:
    output = subprocess.run(
        ["docker", "port", container, internal], check=True, capture_output=True, text=True
    ).stdout
    return output.strip().splitlines()[0].rsplit(":", 1)[1]


@pytest.fixture(scope="module")
def db():
    psycopg = pytest.importorskip("psycopg")

    name = f"grist-migrate-test-{uuid.uuid4().hex[:8]}"
    subprocess.run(
        [
            "docker", "run", "-d", "--rm", "--name", name,
            "-e", "POSTGRES_PASSWORD=secret",
            "-e", "POSTGRES_DB=cublyverse",
            "-p", "127.0.0.1::5432",
            PG_IMAGE,
        ],
        check=True,
        capture_output=True,
    )
    try:
        dsn = (
            f"host=127.0.0.1 port={docker_port(name, '5432/tcp')} "
            "dbname=cublyverse user=postgres password=secret connect_timeout=2"
        )
        deadline = time.time() + 90
        while True:
            try:
                with psycopg.connect(dsn):
                    break
            except psycopg.OperationalError:
                if time.time() > deadline:
                    raise
                time.sleep(0.5)

        workspace_id = str(uuid.uuid4())
        user_id = str(uuid.uuid4())
        with psycopg.connect(dsn) as conn:
            with conn.cursor() as cur:
                for migration in sorted((REPO_ROOT / "internal/store/migrations").glob("*.sql")):
                    cur.execute(goose_up(migration))
                cur.execute(
                    "INSERT INTO users (id, oidc_issuer, oidc_subject)"
                    " VALUES (%s, 'test', 'test')",
                    (user_id,),
                )
                cur.execute(
                    "INSERT INTO workspaces (id, name, created_by, app)"
                    " VALUES (%s, 'Тест', %s, 'warehouse')",
                    (workspace_id, user_id),
                )
                # The needlework template of a new warehouse.
                cur.execute(
                    "INSERT INTO craft_categories"
                    " (workspace_id, slug, name, default_unit, sort_order, catalog_slug)"
                    " SELECT %s, slug, name, default_unit, sort_order, slug"
                    " FROM craft_categories WHERE workspace_id IS NULL",
                    (workspace_id,),
                )
                cur.execute(
                    "INSERT INTO craft_manufacturers (name, website) VALUES ('DMC', '')"
                    " RETURNING id"
                )
                manufacturer_id = cur.fetchone()[0]
                cur.execute(
                    "SELECT id FROM craft_categories"
                    " WHERE slug = 'seed-beads' AND workspace_id IS NULL"
                )
                category_id = cur.fetchone()[0]
                # The official row the imported DMC 1152 must link to. It has no
                # colour name, so the Grist one has to survive on the stock row.
                cur.execute(
                    "INSERT INTO craft_catalog_items ("
                    " workspace_id, category_id, manufacturer_id, article, name,"
                    " color_name, color_hex, color_source, size, finish, unit)"
                    " VALUES (NULL, %s, %s, '1152', 'Официальный DMC 1152',"
                    " '', '#000001', 'official', '', '', 'г')",
                    (category_id, manufacturer_id),
                )
        yield {"dsn": dsn, "workspace_id": workspace_id, "user_id": user_id}
    finally:
        subprocess.run(["docker", "rm", "-f", name], capture_output=True)


def run_migrate(db, *extra):
    result = subprocess.run(
        [
            sys.executable, str(SCRIPT),
            "--from-json", str(FIXTURE),
            "--workspace", db["workspace_id"],
            "--user", db["user_id"],
            "--dsn", db["dsn"],
            *extra,
        ],
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, result.stderr
    return result.stdout


def scalar(conn, sql, params=()):
    with conn.cursor() as cur:
        cur.execute(sql, params)
        return cur.fetchone()[0]


def counts(conn, workspace_id):
    return {
        "manufacturers": scalar(conn, "SELECT count(*) FROM craft_manufacturers"),
        "private_items": scalar(
            conn,
            "SELECT count(*) FROM craft_catalog_items WHERE workspace_id = %s::uuid",
            (workspace_id,),
        ),
        "inventory": scalar(
            conn,
            "SELECT count(*) FROM craft_inventory WHERE workspace_id = %s::uuid",
            (workspace_id,),
        ),
        "logs": scalar(
            conn,
            "SELECT count(*) FROM craft_inventory_log WHERE workspace_id = %s::uuid",
            (workspace_id,),
        ),
        "projects": scalar(
            conn,
            "SELECT count(*) FROM craft_projects WHERE workspace_id = %s::uuid",
            (workspace_id,),
        ),
        "links": scalar(
            conn,
            "SELECT count(*) FROM craft_project_materials m"
            " JOIN craft_projects p ON p.id = m.project_id"
            " WHERE p.workspace_id = %s::uuid",
            (workspace_id,),
        ),
    }


def test_import_is_idempotent(db):
    psycopg = pytest.importorskip("psycopg")
    workspace_id = db["workspace_id"]

    with psycopg.connect(db["dsn"]) as conn:
        before = counts(conn, workspace_id)

    dry = run_migrate(db)
    assert "ИТОГ: сухой прогон" in dry
    with psycopg.connect(db["dsn"]) as conn:
        assert counts(conn, workspace_id) == before

    first = run_migrate(db, "--apply")
    assert "ИТОГ: изменения применены и зафиксированы." in first
    assert "привязано к глобальному 1" in first
    # The official colour wins and is reported, not overwritten.
    assert "color_hex" in first and "#000001" in first

    with psycopg.connect(db["dsn"]) as conn:
        after_first = counts(conn, workspace_id)
        assert after_first == {
            "manufacturers": 3,
            "private_items": 11,
            "inventory": 12,
            "logs": 8,
            "projects": 3,
            "links": 8,
        }
        assert (
            scalar(
                conn,
                "SELECT color_hex FROM craft_catalog_items WHERE workspace_id IS NULL",
            )
            == "#000001"
        )
        # The merged duplicate is stocked once, in the linked official item.
        assert (
            scalar(
                conn,
                "SELECT qty FROM craft_inventory i"
                " JOIN craft_catalog_items c ON c.id = i.catalog_item_id"
                " WHERE i.workspace_id = %s::uuid AND c.workspace_id IS NULL"
                " AND c.article = '1152'",
                (workspace_id,),
            )
            == 12
        )

    with psycopg.connect(db["dsn"]) as conn:
        kept = scalar(
            conn,
            "SELECT i.attrs FROM craft_inventory i"
            " JOIN craft_catalog_items c ON c.id = i.catalog_item_id"
            " WHERE i.workspace_id = %s::uuid AND c.workspace_id IS NULL",
            (workspace_id,),
        )
    # Nothing from Grist is lost on a linked item.
    assert kept["color_name"] and kept["color_hex"] == "#AABBCC"
    assert kept["legacy"]["finish"] == "прозрачный"

    second = run_migrate(db, "--apply")
    assert "создано 0, уже импортировано 11" in second
    with psycopg.connect(db["dsn"]) as conn:
        assert counts(conn, workspace_id) == after_first

    # A dry run on populated data still changes nothing.
    run_migrate(db)
    with psycopg.connect(db["dsn"]) as conn:
        assert counts(conn, workspace_id) == after_first
