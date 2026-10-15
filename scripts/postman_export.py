#!/usr/bin/env python3
"""Build an importable Postman v2.1 collection (.json) from the YAML collection.

The YAML folder (postman/collections/peelen-ms-be) is the source of truth; run
`make postman-export` after editing it and import the generated JSON file.

Usage: postman_export.py <yaml collection dir> <output json>
"""
import json
import os
import sys
from urllib.parse import parse_qsl, urlsplit

import yaml

SCHEMA = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"
EVENTS = {"afterResponse": "test", "http:afterResponse": "test",
          "beforeRequest": "prerequest", "http:beforeRequest": "prerequest"}


def load(path):
    with open(path, encoding="utf-8") as handle:
        return yaml.safe_load(handle) or {}


def events(scripts):
    result = []
    for script in scripts or []:
        listen = EVENTS.get(script.get("type"))
        if listen:
            result.append({"listen": listen, "script": {
                "type": "text/javascript", "exec": script.get("code", "").split("\n")}})
    return result


def build_url(raw_url, params):
    """Postman URL object; params from the YAML win over a query in the URL."""
    base, _, query_string = raw_url.partition("?")
    query = [{"key": k, "value": v} for k, v in parse_qsl(query_string, keep_blank_values=True)]
    for param in params or []:
        entry = {"key": param["key"], "value": str(param.get("value", ""))}
        if param.get("disabled"):
            entry["disabled"] = True
        query.append(entry)
    enabled = [f"{q['key']}={q['value']}" for q in query if not q.get("disabled")]
    raw = base + ("?" + "&".join(enabled) if enabled else "")
    host, _, path = base.partition("}}")
    url = {"raw": raw, "host": [host + "}}"] if path else [base],
           "path": [p for p in path.split("/") if p]}
    if not path:  # URL without {{base_url}}
        parts = urlsplit(base)
        url["host"] = [parts.netloc] if parts.netloc else [base]
        url["path"] = [p for p in parts.path.split("/") if p]
    if query:
        url["query"] = query
    return url


def build_body(body):
    if not body:
        return None
    kind = body.get("type")
    if kind == "json":
        return {"mode": "raw", "raw": body.get("content", ""), "options": {"raw": {"language": "json"}}}
    if kind in ("form-data", "urlencoded"):
        fields = []
        for field in body.get("content") or []:
            entry = {"key": field["key"], "type": field.get("type", "text")}
            if entry["type"] == "file":
                entry["src"] = field.get("value") or []
            else:
                entry["value"] = str(field.get("value", ""))
            if field.get("disabled"):
                entry["disabled"] = True
            fields.append(entry)
        mode = "formdata" if kind == "form-data" else "urlencoded"
        return {"mode": mode, mode: fields}
    return {"mode": "raw", "raw": str(body.get("content", ""))}


def build_request(path):
    spec = load(path)
    name = os.path.basename(path)[: -len(".request.yaml")]
    request = {
        "method": spec.get("method", "GET"),
        "header": [{"key": k, "value": str(v)} for k, v in (spec.get("headers") or {}).items()],
        "url": build_url(spec.get("url", ""), spec.get("params")),
    }
    body = build_body(spec.get("body"))
    if body:
        request["body"] = body
    if spec.get("description"):
        request["description"] = spec["description"]
    item = {"name": name, "request": request}
    if spec.get("scripts"):
        item["event"] = events(spec["scripts"])
    return spec.get("order", 10**9), name, item


def build_folder(directory):
    """Items of a folder, ordered by their `order` field, then by name."""
    entries = []
    for entry in os.listdir(directory):
        full = os.path.join(directory, entry)
        if entry.endswith(".request.yaml"):
            entries.append(build_request(full))
        elif os.path.isdir(full) and not entry.startswith("."):
            definition = os.path.join(full, ".resources", "definition.yaml")
            spec = load(definition) if os.path.exists(definition) else {}
            folder = {"name": entry, "item": build_folder(full)}
            if spec.get("description"):
                folder["description"] = spec["description"]
            entries.append((spec.get("order", 10**9), entry, folder))
    return [item for _, _, item in sorted(entries, key=lambda e: (e[0], e[1].lower()))]


def build_collection(directory):
    root = load(os.path.join(directory, ".resources", "definition.yaml"))
    name = os.path.basename(os.path.normpath(directory))
    collection = {
        "info": {"name": name, "description": root.get("description", ""), "schema": SCHEMA},
        "item": build_folder(directory),
        "variable": [{"key": k, "value": "" if v is None else str(v)}
                     for k, v in (root.get("variables") or {}).items()],
    }
    for auth in root.get("auth") or []:
        if auth.get("type") == "bearer":
            token = (auth.get("credentials") or {}).get("token", "")
            collection["auth"] = {"type": "bearer", "bearer": [{"key": "token", "value": token, "type": "string"}]}
            break
    if root.get("scripts"):
        collection["event"] = events(root["scripts"])
    return collection


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    collection = build_collection(sys.argv[1])
    with open(sys.argv[2], "w", encoding="utf-8") as handle:
        json.dump(collection, handle, indent=2, ensure_ascii=False)
        handle.write("\n")
    count = sum(len(f.get("item", [f])) for f in collection["item"])
    print(f"wrote {sys.argv[2]}: {len(collection['item'])} top-level entries, {count} requests")


if __name__ == "__main__":
    main()
