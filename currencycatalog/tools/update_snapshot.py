#!/usr/bin/env python3
"""Generate a dated currency catalog snapshot from an official SIX List One XML file."""

import argparse
import json
import re
import sys
import xml.etree.ElementTree as ET
from pathlib import Path

SIX_LIST_ONE_URL = (
    "https://www.six-group.com/dam/download/financial-information/"
    "data-center/iso-currrency/lists/list-one.xml"
)
SPECIAL_CODES = {
    "XAU", "XAG", "XPD", "XPT",  # Precious metals
    "XBA", "XBB", "XBC", "XBD",  # Bond market composite units
    "XDR", "XSU", "XUA", "XTS", "XXX",  # Special, test, or no-currency codes
}


def generate(source: Path, output_dir: Path) -> Path:
    root = ET.parse(source).getroot()
    published = root.attrib.get("Pblshd", "")
    if not re.fullmatch(r"\d{4}-\d{2}-\d{2}", published):
        raise ValueError("SIX XML is missing a valid Pblshd date")

    currencies: dict[str, str] = {}
    for entry in root.findall(".//CcyNtry"):
        code = (entry.findtext("Ccy") or "").strip()
        name_element = entry.find("CcyNm")
        name = (name_element.text or "").strip() if name_element is not None else ""
        minor_units = (entry.findtext("CcyMnrUnts") or "").strip()

        if not re.fullmatch(r"[A-Z]{3}", code) or not name:
            continue
        if name_element is not None and name_element.attrib.get("IsFund", "").lower() == "true":
            continue
        if not re.fullmatch(r"\d+", minor_units) or code in SPECIAL_CODES:
            continue

        previous_name = currencies.setdefault(code, name)
        if previous_name != name:
            raise ValueError(f"SIX XML assigns conflicting names to {code}: {previous_name!r}, {name!r}")

    if not currencies:
        raise ValueError("No monetary currencies found in SIX XML")

    payload = {
        "source": SIX_LIST_ONE_URL,
        "published": published,
        "currencies": [
            {"code": code, "name": currencies[code]} for code in sorted(currencies)
        ],
    }
    output_dir.mkdir(parents=True, exist_ok=True)
    destination = output_dir / f"{published}.json"
    destination.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return destination


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("six_xml", type=Path, help="path to a locally downloaded official SIX List One XML")
    parser.add_argument(
        "--output-dir",
        type=Path,
        default=Path(__file__).resolve().parents[1] / "snapshots",
        help="directory for the dated JSON snapshot",
    )
    args = parser.parse_args()
    try:
        destination = generate(args.six_xml, args.output_dir)
    except (OSError, ET.ParseError, ValueError) as exc:
        print(f"update_snapshot: {exc}", file=sys.stderr)
        return 1
    print(destination)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
