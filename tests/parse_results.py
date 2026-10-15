#!/usr/bin/env python3
"""
parse_results.py — converts a Newman JSON report into a detailed CSV.

Usage:
    python3 parse_results.py <newman_report.json> <ROLE> <output.csv>

CSV columns:
    Test ID, Folder, Test Name, Role, Method, URL,
    Request Headers, Request Body,
    HTTP Status, Response Body,
    Test Assertions, Result

Folder resolution strategy:
    Newman does not embed parent folder info in execution items. We build an
    ordered list from the collection file (same directory as the report, or
    found via COLLECTION_PATH env var) and match by position. The one known
    duplicate execution (the 'Change Password Success' pm.sendRequest inner
    call) is detected and skipped in the position cursor.
"""

import csv
import json
import os
import re
import sys
from pathlib import Path


COLLECTION_PATH = os.environ.get(
    "COLLECTION_PATH",
    str(Path(__file__).parent.parent / "postman" / "peelen-ms-be-tests.postman_collection.json"),
)


def build_ordered_items(collection_path: str) -> list[dict]:
    """Return ordered list of {folder, name} dicts matching collection traversal order."""
    with open(collection_path, encoding="utf-8") as f:
        col = json.load(f)

    ordered: list[dict] = []

    def walk(items: list, folder_path: str = "") -> None:
        for item in items:
            name = item.get("name", "")
            if "item" in item:
                new_path = f"{folder_path} > {name}" if folder_path else name
                walk(item["item"], new_path)
            else:
                ordered.append({"folder": folder_path, "name": name})

    walk(col.get("item", []))
    return ordered


def redact_auth(headers: list) -> str:
    """Return header string with Authorization token masked."""
    parts = []
    for h in headers:
        key = h.get("key", "")
        val = h.get("value", "")
        if key.lower() == "authorization" and val:
            val = re.sub(r"(Bearer\s+)\S+", r"\1[REDACTED]", val)
        parts.append(f"{key}: {val}")
    return " | ".join(parts)


def body_str(body: dict | None) -> str:
    if not body:
        return ""
    mode = body.get("mode", "")
    if mode == "raw":
        return (body.get("raw") or "").strip()
    if mode == "formdata":
        items = []
        for fd in body.get("formdata", []):
            if fd.get("type") == "file":
                items.append(f'{fd["key"]}=[FILE]')
            else:
                items.append(f'{fd["key"]}={fd.get("value", "")}')
        return " | ".join(items)
    if mode == "urlencoded":
        return "&".join(f'{x["key"]}={x.get("value", "")}' for x in body.get("urlencoded", []))
    return f"[{mode}]"


def truncate(s: str, n: int = 600) -> str:
    s = s.replace("\n", " ").replace("\r", "").strip()
    return (s[:n] + "…") if len(s) > n else s


def parse_report(report_path: str, role: str, output_path: str) -> None:
    with open(report_path, encoding="utf-8") as f:
        report = json.load(f)

    # Build folder map from collection (positional matching)
    try:
        col_items = build_ordered_items(COLLECTION_PATH)
    except FileNotFoundError:
        col_items = []

    rows = []
    col_cursor = 0          # position in the collection ordered list
    seen_change_pw = False  # handle the one duplicated execution

    executions = report.get("run", {}).get("executions", [])
    for test_id, exec_item in enumerate(executions, start=1):
        item = exec_item.get("item", {})
        newman_name = item.get("name", "")

        # ── Resolve folder via positional matching ─────────────────────────
        # The "Change Password Success" test fires twice in Newman because the
        # test script uses pm.sendRequest internally — skip the second occurrence
        # in the position cursor so subsequent items stay aligned.
        if newman_name == "200 - Change Password Success" and seen_change_pw:
            folder = "Auth - Change Password"
            test_name = newman_name
            # do NOT advance col_cursor
        else:
            if newman_name == "200 - Change Password Success":
                seen_change_pw = True
            if col_cursor < len(col_items) and col_items[col_cursor]["name"] == newman_name:
                folder = col_items[col_cursor]["folder"]
                test_name = col_items[col_cursor]["name"]
                col_cursor += 1
            else:
                # Fallback: try to find it ahead (drift recovery)
                matched = False
                for lookahead in range(col_cursor, min(col_cursor + 5, len(col_items))):
                    if col_items[lookahead]["name"] == newman_name:
                        folder = col_items[lookahead]["folder"]
                        test_name = col_items[lookahead]["name"]
                        col_cursor = lookahead + 1
                        matched = True
                        break
                if not matched:
                    folder = ""
                    test_name = newman_name

        # ── Request ────────────────────────────────────────────────────────
        request_obj = exec_item.get("request", {})
        method = request_obj.get("method", "")
        url_obj = request_obj.get("url", {})
        if isinstance(url_obj, dict):
            url = url_obj.get("raw", "") or "/".join([""] + url_obj.get("path", []))
        else:
            url = str(url_obj)

        req_headers = redact_auth(request_obj.get("header", []))
        req_body = body_str(request_obj.get("body"))

        # ── Response ───────────────────────────────────────────────────────
        response_obj = exec_item.get("response", {})
        http_status = response_obj.get("code", "")
        resp_body_raw = response_obj.get("body") or response_obj.get("stream", {})

        # Newman stores body as {"type":"Buffer","data":[...]} — decode it
        if isinstance(resp_body_raw, dict) and resp_body_raw.get("type") == "Buffer":
            try:
                decoded = bytes(resp_body_raw["data"]).decode("utf-8", errors="replace")
                # Pretty-print if JSON
                try:
                    resp_body = truncate(json.dumps(json.loads(decoded)))
                except Exception:
                    resp_body = truncate(decoded)
            except Exception:
                resp_body = truncate(str(resp_body_raw))
        elif isinstance(resp_body_raw, dict):
            resp_body = truncate(json.dumps(resp_body_raw))
        elif resp_body_raw:
            try:
                resp_body = truncate(json.dumps(json.loads(str(resp_body_raw))))
            except Exception:
                resp_body = truncate(str(resp_body_raw))
        else:
            resp_body = ""

        # ── Test assertions ────────────────────────────────────────────────
        assertions = exec_item.get("assertions", []) or []
        assertion_parts = []
        overall = "PASS"

        for a in assertions:
            a_name = a.get("assertion", "")
            err = a.get("error")
            if err:
                assertion_parts.append(f"FAIL: {a_name} ({err.get('message', '')})")
                overall = "FAIL"
            else:
                assertion_parts.append(f"PASS: {a_name}")

        if exec_item.get("requestError"):
            overall = "FAIL"
            assertion_parts.insert(0, f"ERROR: {exec_item['requestError']}")

        if not assertions and not exec_item.get("requestError"):
            overall = "SKIP"

        rows.append({
            "Test ID": test_id,
            "Folder": folder,
            "Test Name": test_name,
            "Role": role,
            "Method": method,
            "URL": url,
            "Request Headers": req_headers,
            "Request Body": req_body,
            "HTTP Status": http_status,
            "Response Body": resp_body,
            "Test Assertions": " || ".join(assertion_parts),
            "Result": overall,
        })

    # ── Write CSV ──────────────────────────────────────────────────────────
    fieldnames = [
        "Test ID", "Folder", "Test Name", "Role", "Method", "URL",
        "Request Headers", "Request Body",
        "HTTP Status", "Response Body",
        "Test Assertions", "Result",
    ]
    with open(output_path, "w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=fieldnames)
        writer.writeheader()
        writer.writerows(rows)

    total = len(rows)
    passed = sum(1 for r in rows if r["Result"] == "PASS")
    failed = sum(1 for r in rows if r["Result"] == "FAIL")
    skipped = sum(1 for r in rows if r["Result"] == "SKIP")
    print(f"[parser] {role}: {total} tests → {passed} PASS, {failed} FAIL, {skipped} SKIP")


if __name__ == "__main__":
    if len(sys.argv) != 4:
        print("Usage: parse_results.py <report.json> <ROLE> <output.csv>")
        sys.exit(1)
    parse_report(sys.argv[1], sys.argv[2], sys.argv[3])
