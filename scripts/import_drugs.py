#!/usr/bin/env python3
"""
Convert a country-specific source CSV into <data-dir>/<country>/drugs.db.

Usage (run from backend/):
    python3 ../scripts/import_drugs.py --country ua
    python3 ../scripts/import_drugs.py --country ua --source ua-drlz

Adding a country = adding an entry to SOURCES below, dropping the source CSV
into backend/data/<country>/, and re-running. Schema and Go side never change.
"""

import argparse
import csv
import os
import re
import sqlite3
import sys
from typing import Callable, Optional

MappedRow = dict[str, Optional[str]]
RowMapper = Callable[[dict[str, str]], MappedRow]

# Glyphs that add visual noise to a trade name and never carry meaning.
_NOISE_GLYPHS = re.compile(r"[®™©℠]")
_WHITESPACE = re.compile(r"\s+")


def clean_text(s: Optional[str]) -> Optional[str]:
    if not s:
        return None
    s = _NOISE_GLYPHS.sub("", s)
    s = _WHITESPACE.sub(" ", s).strip()
    return s or None


def first_sentence(s: Optional[str]) -> Optional[str]:
    if not s:
        return None
    return s.split(".", 1)[0].strip() or None


def _ua_drlz(row: dict[str, str]) -> MappedRow:
    return {
        "name":         row.get("Торгівельне найменування", ""),
        "inn":          row.get("Міжнародне непатентоване найменування", ""),
        "ingredients":  row.get("Склад (діючі)", ""),
        "pharma_group": first_sentence(row.get("Фармакотерапевтична група 1", "")),
    }


SOURCES: dict[str, dict] = {
    "ua-drlz": {
        "csv_dialect": {"delimiter": ";", "encoding": "utf-8-sig"},
        "map": _ua_drlz,
    },
}

# Default --source per --country. Lets users skip --source for the common case.
DEFAULT_SOURCE: dict[str, str] = {"ua": "ua-drlz"}


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--country", required=True, help="ISO country code, e.g. ua, gb")
    parser.add_argument("--source", help="Source key from SOURCES (defaults per country)")
    parser.add_argument("--data-dir", default="data", help="Base data directory")
    parser.add_argument(
        "--csv-name",
        default="parsed_medicinal_products.csv",
        help="Source CSV filename inside <data-dir>/<country>/",
    )
    args = parser.parse_args()

    source_key = args.source or DEFAULT_SOURCE.get(args.country)
    if source_key is None:
        print(f"no default source for country '{args.country}'; pass --source", file=sys.stderr)
        sys.exit(1)
    source = SOURCES.get(source_key)
    if source is None:
        print(f"unknown source '{source_key}'; available: {sorted(SOURCES)}", file=sys.stderr)
        sys.exit(1)

    country_dir = os.path.join(args.data_dir, args.country)
    input_path = os.path.join(country_dir, args.csv_name)
    output_path = os.path.join(country_dir, "drugs.db")
    if not os.path.isfile(input_path):
        print(f"source not found: {input_path}", file=sys.stderr)
        sys.exit(1)

    conn = sqlite3.connect(output_path)
    conn.execute("DROP TABLE IF EXISTS drugs")
    conn.execute(
        """
        CREATE TABLE drugs (
            id            INTEGER PRIMARY KEY AUTOINCREMENT,
            name          TEXT NOT NULL,
            inn           TEXT,
            ingredients   TEXT,
            pharma_group  TEXT
        )
        """
    )
    conn.execute("CREATE INDEX idx_drugs_name ON drugs(name COLLATE NOCASE)")

    seen: set[str] = set()
    rows: list[tuple[Optional[str], ...]] = []
    dialect = source["csv_dialect"]
    mapper: RowMapper = source["map"]

    with open(input_path, encoding=dialect["encoding"], newline="") as f:
        reader = csv.DictReader(f, delimiter=dialect["delimiter"])
        for raw in reader:
            mapped = mapper(raw)
            name = clean_text(mapped["name"])
            if not name or name in seen:
                continue
            seen.add(name)
            rows.append((
                name,
                clean_text(mapped.get("inn")),
                clean_text(mapped.get("ingredients")),
                clean_text(mapped.get("pharma_group")),
            ))

    conn.executemany(
        "INSERT INTO drugs (name, inn, ingredients, pharma_group) VALUES (?, ?, ?, ?)",
        rows,
    )
    conn.commit()

    count = conn.execute("SELECT COUNT(*) FROM drugs").fetchone()[0]
    print(f"Imported {count} unique drugs → {output_path}", file=sys.stderr)
    conn.close()


if __name__ == "__main__":
    main()
