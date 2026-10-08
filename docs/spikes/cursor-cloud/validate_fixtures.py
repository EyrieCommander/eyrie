#!/usr/bin/env python3
"""Validate fixtures/*.json against Cursor's Cloud Agents OpenAPI spec.

Usage: python3 validate_fixtures.py [path/to/cloud-agents-openapi.yaml]

Without an argument it downloads the spec from cursor.com into a temporary
file. Either way the spec's sha256 must equal PINNED_SHA256 (the snapshot
the report was written against); a different spec stops the check rather
than validating against something else. The spec is not vendored here: it
is Cursor's document, fetched read-only. Needs PyYAML and jsonschema.
Exit 0 only if every fixture validates AND the negative controls fail.
"""
import copy, glob, hashlib, json, os, sys, tempfile, urllib.request

import jsonschema
import yaml

SPEC_URL = "https://cursor.com/docs-static/cloud-agents-openapi.yaml"
PINNED_SHA256 = "664e695207e9f72dd7dd296d60b576e68575f8e71ce18e6263a554850c072570"
HERE = os.path.dirname(os.path.abspath(__file__))

# fixture file -> OpenAPI component schema
SCHEMA_FOR = {
    "create-agent.request.json": "CreateAgentRequest",
    "create-agent.response.json": "CreateAgentResponse",
    "get-run.running.json": "Run",
    "get-run.finished.json": "Run",
    "get-run.error.json": "Run",
    "get-run.expired.json": "Run",
    "get-run.cancelled.json": "Run",
    "cancel-run.response.json": "IdResponse",
    "error.run-not-cancellable.409.json": "Error",
    "error.agent-id-conflict.409.json": "Error",
    "error.invalid-model.400.json": "Error",
    "error.rate-limited.429.json": "Error",
    "agent-usage.response.json": "AgentUsageResponse",
    "list-models.response.json": "ListModelsResponse",
}


def load_spec(path):
    if path is None:
        req = urllib.request.Request(SPEC_URL, headers={"User-Agent": "Mozilla/5.0"})
        with urllib.request.urlopen(req, timeout=30) as r:
            raw = r.read()
    else:
        raw = open(path, "rb").read()
    got = hashlib.sha256(raw).hexdigest()
    if got != PINNED_SHA256:
        sys.exit(f"spec sha256 {got} != pinned {PINNED_SHA256}: the spec changed; re-check the report first")
    return yaml.safe_load(raw)


def to_jsonschema(o):
    """OpenAPI 3.0 schema objects -> draft 2020-12 ($ref and nullable)."""
    if isinstance(o, dict):
        o = {k: to_jsonschema(v) for k, v in o.items()}
        if o.pop("nullable", False) and "type" in o:
            o["type"] = [o["type"], "null"]
        if "$ref" in o:
            o["$ref"] = o["$ref"].replace("#/components/schemas/", "#/$defs/")
        return o
    if isinstance(o, list):
        return [to_jsonschema(x) for x in o]
    return o


def main():
    spec = load_spec(sys.argv[1] if len(sys.argv) > 1 else None)
    defs = to_jsonschema(copy.deepcopy(spec["components"]["schemas"]))

    def errors(name, doc):
        v = jsonschema.Draft202012Validator({"$ref": "#/$defs/" + name, "$defs": defs})
        return [e.message for e in v.iter_errors(doc)]

    files = sorted(os.path.basename(p) for p in glob.glob(os.path.join(HERE, "fixtures", "*.json")))
    missing = set(SCHEMA_FOR) - set(files)
    extra = set(files) - set(SCHEMA_FOR)
    if missing or extra:
        sys.exit(f"fixture set mismatch: missing {sorted(missing)}, unmapped {sorted(extra)}")
    bad = 0
    for f in files:
        doc = json.load(open(os.path.join(HERE, "fixtures", f)))
        errs = errors(SCHEMA_FOR[f], doc)
        print(("ok   " if not errs else "FAIL ") + f + ("" if not errs else ": " + "; ".join(errs[:2])))
        bad += bool(errs)
    # Negative controls: the check must reject documents the spec forbids.
    run = json.load(open(os.path.join(HERE, "fixtures", "get-run.running.json")))
    controls = {
        "run with undocumented status": ("Run", dict(run, status="DONE")),
        "run without agentId": ("Run", {k: v for k, v in run.items() if k != "agentId"}),
        "create without prompt": ("CreateAgentRequest", {"model": {"id": "composer-2"}}),
    }
    for label, (name, doc) in controls.items():
        rejected = bool(errors(name, doc))
        print(("ok   " if rejected else "FAIL ") + "negative control rejected: " + label)
        bad += not rejected
    print(f"{len(files)} fixtures, {len(controls)} negative controls, {bad} problems")
    sys.exit(1 if bad else 0)


if __name__ == "__main__":
    main()
