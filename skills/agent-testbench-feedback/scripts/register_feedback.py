#!/usr/bin/env python3
"""Append a durable AgentTestBench feedback entry."""

from __future__ import annotations

import argparse
from datetime import datetime, timezone
from pathlib import Path
import textwrap


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--title", required=True)
    parser.add_argument("--area", required=True)
    parser.add_argument("--severity", required=True)
    parser.add_argument("--source", required=True)
    parser.add_argument("--evidence", required=True)
    parser.add_argument("--suggestion", required=True)
    parser.add_argument("--status", default="new")
    return parser.parse_args()


def bullet(label: str, value: str) -> str:
    clean = " ".join(value.split())
    return f"- {label}: {clean}\n"


def main() -> int:
    args = parse_args()
    root = Path(__file__).resolve().parents[1]
    ledger = root / "feedback.md"
    if not ledger.exists():
        ledger.write_text(
            "# AgentTestBench Feedback\n\n"
            "Durable feedback registered by local Codex sessions.\n",
            encoding="utf-8",
        )

    today = datetime.now(timezone.utc).strftime("%Y-%m-%d")
    entry = "\n" + textwrap.dedent(
        f"""\
        ## {today} - {" ".join(args.title.split())}
        """
    )
    entry += bullet("Area", args.area)
    entry += bullet("Severity", args.severity)
    entry += bullet("Status", args.status)
    entry += bullet("Source", args.source)
    entry += bullet("Evidence", args.evidence)
    entry += bullet("Suggestion", args.suggestion)
    ledger.write_text(ledger.read_text(encoding="utf-8").rstrip() + entry, encoding="utf-8")
    print(ledger)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
