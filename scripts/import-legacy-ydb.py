#!/usr/bin/env python3
"""Import the pinned YDB SQL regression corpus.

This is an import utility, not a dependency of builds or tests. The destination
contains everything needed to use the imported material after the fork is gone.
"""

import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
import subprocess


REVISION = "8eed5d890396eb03953248a3ec4ab7e28dfaed45"
PREFIX = "internal/endtoend/testdata/"


def digest(data):
    return hashlib.sha256(data).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("repository", type=Path, help="local checkout of the old sqlc fork")
    parser.add_argument("--out", type=Path, default=Path(__file__).resolve().parents[1] / "testdata/legacy-ydb")
    args = parser.parse_args()

    def git(*argv):
        return subprocess.check_output(["git", "-C", str(args.repository), *argv])

    def source(path):
        return git("show", REVISION + ":" + path)

    paths = git("ls-tree", "-r", "--name-only", REVISION, "--", PREFIX).decode().splitlines()
    configs = [p for p in paths if PurePosixPath(p).name in ("sqlc.json", "sqlc.yaml", "sqlc.yml")
               and ("/ydb/" in p or "/ydb-go-sdk/" in p)]
    args.out.mkdir(parents=True, exist_ok=True)
    imported = {}

    def write(relative, data, original=None):
        target = args.out / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)
        imported[relative] = {"sha256": digest(data)}
        if original:
            imported[relative]["source"] = original

    cases = {}
    scenarios = set()
    for config_path in configs:
        raw = source(config_path)
        if config_path.endswith(".json"):
            config = json.loads(raw)
            entries = config.get("sql", config.get("packages", []))
        else:
            # The pinned corpus has one simple YAML config. Deliberately fail
            # on anything else instead of pretending to parse arbitrary YAML.
            if config_path != PREFIX + "params_in_nested_func/ydb/sqlc.yaml":
                raise ValueError("unexpected YAML config: " + config_path)
            fields = {key: re.findall(rb"(?:^|\n)\s*(?:-\s*)?" + key.encode() + rb":\s*([^\n]+)", raw)
                      for key in ("schema", "queries", "engine")}
            if any(len(values) != 1 for values in fields.values()):
                raise ValueError("unexpected pinned YAML shape: " + config_path)
            entries = [{key: values[0].decode().strip() for key, values in fields.items()}]
        directory = str(PurePosixPath(config_path).parent)
        scenario = config_path[len(PREFIX):].split("/")[0]
        scenarios.add(scenario)
        write("inputs/" + config_path[len(PREFIX):], raw, config_path)
        for entry in entries:
            if entry.get("engine") != "ydb":
                raise ValueError("non-YDB entry: " + config_path)
            files = {}
            for role in ("schema", "queries"):
                patterns = entry[role]
                if isinstance(patterns, str):
                    patterns = [patterns]
                selected = []
                for pattern in patterns:
                    path = str(PurePosixPath(directory) / pattern)
                    if path in paths:
                        selected.append(path)
                    else:
                        selected.extend(p for p in paths if str(PurePosixPath(p).parent) == path and p.endswith(".sql"))
                if not selected:
                    raise ValueError("no " + role + " sources: " + config_path)
                files[role] = list(dict.fromkeys(sorted(selected)))
            # Old sqlc tolerated the query.sql located inside datatype's schema
            # directory. Keep the file, but feed only DDL files to the catalog.
            overlap = set(files["schema"]) & set(files["queries"])
            files["schema"] = [p for p in files["schema"] if p not in overlap]
            contents = {role: [source(p) for p in files[role]] for role in ("schema", "queries")}
            key = digest(json.dumps({role: [digest(v) for v in contents[role]] for role in contents}, sort_keys=True).encode())
            for role in files:
                for path, data in zip(files[role], contents[role]):
                    write("inputs/" + path[len(PREFIX):], data, path)
            if key in cases:
                cases[key]["source_configs"].append(config_path)
                continue
            cases[key] = {
                "name": scenario + "-" + key[:10],
                "schema": ["inputs/" + p[len(PREFIX):] for p in files["schema"]],
                "queries": ["inputs/" + p[len(PREFIX):] for p in files["queries"]],
                "source_configs": [config_path],
            }
            if overlap:
                cases[key]["adaptations"] = ["Query inputs excluded from the schema directory; SQL bytes unchanged."]

    manifest = {
        "repository": "https://github.com/ydb-platform/sqlc",
        "revision": REVISION,
        "scenario_count": len(scenarios),
        "configuration_count": len(configs),
        "case_count": len(cases),
        "files": dict(sorted(imported.items())),
        "cases": sorted(cases.values(), key=lambda case: case["name"]),
    }
    (args.out / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    print("Preserved {} scenarios, {} configurations, {} distinct SQL inputs, {} files".format(
        len(scenarios), len(configs), len(cases), len(imported)))


if __name__ == "__main__":
    main()
