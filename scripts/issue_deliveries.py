#!/usr/bin/env python3
"""Report issue delivery metadata for the current Multica workspace.

This is a read-only report, not a completion gate. It fetches every issue with
``multica issue list`` in 50-item pages, then prints one row per issue carrying
delivery metadata. Run ``scripts/issue_deliveries.py`` for the terminal table or
``scripts/issue_deliveries.py --json`` for machine-readable output. The columns
are issue key/status, repository resource, base and delivery refs/SHAs, primary
MR URL, stack parent, and diagnostic flags. During the transition,
``baseline_ref`` and ``delivery_branch`` are accepted as fallbacks but shown as
deprecated. A primary MR is confirmed only when the flat metadata key
``vcs.primary_mr_confirmed`` is the boolean value ``true``.

Findings are included in either output format. They do not change the successful
exit status; only an inability to fetch or parse the workspace is an error.
"""

import argparse
import json
import subprocess
import sys
from collections import Counter, defaultdict


PAGE_LIMIT = 50
MR_CONFIRMATION_KEY = "vcs.primary_mr_confirmed"

FIELD_KEYS = {
    "repo_resource_id": "git.repo_resource_id",
    "base_ref": "git.base_ref",
    "base_sha": "git.base_sha",
    "delivery_ref": "git.delivery_ref",
    "delivery_sha": "git.delivery_sha",
    "primary_mr_url": "vcs.primary_mr_url",
    "stack_parent_issue": "git.stack_parent_issue",
}
LEGACY_KEYS = {
    "base_ref": "baseline_ref",
    "delivery_ref": "delivery_branch",
}
DELIVERY_KEYS = set(FIELD_KEYS.values()) | set(LEGACY_KEYS.values())


class ReportError(RuntimeError):
    """A workspace report could not be produced."""


def run_multica(args, executable="multica"):
    """Run the CLI and decode its JSON response."""
    command = [executable] + args
    proc = subprocess.run(command, capture_output=True, text=True)
    if proc.returncode != 0:
        detail = proc.stderr.strip() or proc.stdout.strip() or "unknown error"
        raise ReportError("%s failed: %s" % (" ".join(command), detail))
    try:
        return json.loads(proc.stdout)
    except json.JSONDecodeError as exc:
        raise ReportError("%s returned invalid JSON: %s" % (" ".join(command), exc))


def fetch_all_issues(run=run_multica, page_limit=PAGE_LIMIT):
    """Fetch all pages, honoring the response's offset, limit, and has_more."""
    issues = []
    offset = 0
    pages = 0

    while True:
        payload = run([
            "issue", "list",
            "--limit", str(page_limit),
            "--offset", str(offset),
            "--output", "json",
        ])
        if not isinstance(payload, dict) or not isinstance(payload.get("issues"), list):
            raise ReportError("issue list response must contain an issues array")

        page = payload["issues"]
        issues.extend(page)
        pages += 1
        if payload.get("has_more") is not True:
            break

        response_offset = payload.get("offset")
        response_limit = payload.get("limit")
        if not isinstance(response_offset, int) or not isinstance(response_limit, int):
            raise ReportError("paginated issue list response is missing integer offset/limit")
        next_offset = response_offset + response_limit
        if not page or next_offset <= offset:
            raise ReportError("issue list pagination made no progress at offset %d" % offset)
        offset = next_offset

    return issues, pages


def _text_value(metadata, key):
    value = metadata.get(key)
    return value.strip() if isinstance(value, str) and value.strip() else None


def build_entries(issues):
    """Normalize current and legacy metadata into sorted ledger entries."""
    entries = []
    for issue in issues:
        metadata = issue.get("metadata") or {}
        if not isinstance(metadata, dict) or not (DELIVERY_KEYS & set(metadata)):
            continue

        values = {}
        sources = {}
        conflicts = []
        for field, current_key in FIELD_KEYS.items():
            current = _text_value(metadata, current_key)
            legacy_key = LEGACY_KEYS.get(field)
            legacy = _text_value(metadata, legacy_key) if legacy_key else None
            values[field] = current if current is not None else legacy
            if current is not None:
                sources[field] = current_key
            elif legacy is not None:
                sources[field] = legacy_key
            if current is not None and legacy is not None and current != legacy:
                conflicts.append({
                    "field": field,
                    "current_key": current_key,
                    "current_value": current,
                    "legacy_key": legacy_key,
                    "legacy_value": legacy,
                })

        deprecated_keys = sorted(key for key in LEGACY_KEYS.values() if key in metadata)
        entries.append({
            "key": issue.get("identifier") or issue.get("id") or "<unknown>",
            "id": issue.get("id"),
            "title": issue.get("title") or "",
            "status": issue.get("status") or "",
            **values,
            "primary_mr_confirmed": metadata.get(MR_CONFIRMATION_KEY) is True,
            "sources": sources,
            "deprecated_keys": deprecated_keys,
            "metadata_conflicts": conflicts,
        })

    return sorted(entries, key=lambda entry: entry["key"])


def _canonical_cycle(nodes):
    """Rotate a cycle to its lexicographically smallest issue key."""
    pivot = min(range(len(nodes)), key=lambda index: nodes[index])
    return tuple(nodes[pivot:] + nodes[:pivot])


def find_stack_cycles(entries):
    """Return unique cycles in the one-parent-per-issue stack graph."""
    aliases = {}
    for entry in entries:
        aliases[entry["key"].lower()] = entry["key"]
        if entry["id"]:
            aliases[entry["id"].lower()] = entry["key"]

    graph = {}
    for entry in entries:
        parent = entry["stack_parent_issue"]
        if parent:
            resolved = aliases.get(parent.lower())
            if resolved:
                graph[entry["key"]] = resolved

    cycles = set()
    exhausted = set()
    for start in sorted(graph):
        path = []
        positions = {}
        node = start
        while node in graph and node not in exhausted:
            if node in positions:
                cycle = path[positions[node]:]
                cycles.add(_canonical_cycle(cycle))
                break
            positions[node] = len(path)
            path.append(node)
            node = graph[node]
        exhausted.update(path)

    return [list(cycle) + [cycle[0]] for cycle in sorted(cycles)]


def analyze(entries):
    """Produce deterministic, non-gating findings for ledger inconsistencies."""
    findings = []
    for entry in entries:
        key = entry["key"]
        if entry["delivery_ref"] and not entry["base_ref"]:
            findings.append({
                "type": "missing_base_ref",
                "issue_keys": [key],
                "message": "%s has a delivery ref but no base ref" % key,
            })
        if entry["base_ref"] and not entry["delivery_ref"]:
            findings.append({
                "type": "missing_delivery_ref",
                "issue_keys": [key],
                "message": "%s has a base ref but no delivery ref" % key,
            })
        if entry["primary_mr_url"] and not entry["primary_mr_confirmed"]:
            findings.append({
                "type": "unconfirmed_mr",
                "issue_keys": [key],
                "message": "%s has a primary MR URL without %s=true" % (
                    key, MR_CONFIRMATION_KEY),
            })
        if entry["deprecated_keys"]:
            findings.append({
                "type": "deprecated_keys",
                "issue_keys": [key],
                "metadata_keys": entry["deprecated_keys"],
                "message": "%s uses deprecated metadata: %s" % (
                    key, ", ".join(entry["deprecated_keys"])),
            })
        for conflict in entry["metadata_conflicts"]:
            findings.append({
                "type": "metadata_conflict",
                "issue_keys": [key],
                **conflict,
                "message": "%s has conflicting %s and %s values" % (
                    key, conflict["current_key"], conflict["legacy_key"]),
            })

    by_delivery_ref = defaultdict(list)
    for entry in entries:
        if entry["delivery_ref"]:
            by_delivery_ref[entry["delivery_ref"]].append(entry["key"])
    for delivery_ref, issue_keys in sorted(by_delivery_ref.items()):
        if len(issue_keys) > 1:
            issue_keys = sorted(issue_keys)
            findings.append({
                "type": "duplicate_delivery_ref",
                "issue_keys": issue_keys,
                "delivery_ref": delivery_ref,
                "message": "%s is used by %s" % (
                    delivery_ref, ", ".join(issue_keys)),
            })

    for cycle in find_stack_cycles(entries):
        findings.append({
            "type": "stack_cycle",
            "issue_keys": cycle[:-1],
            "cycle": cycle,
            "message": "stack cycle: %s" % " -> ".join(cycle),
        })

    return sorted(
        findings,
        key=lambda finding: (
            finding["type"],
            tuple(finding.get("issue_keys", [])),
            finding.get("message", ""),
        ),
    )


def make_report(issues, pages):
    entries = build_entries(issues)
    findings = analyze(entries)
    counts = Counter(finding["type"] for finding in findings)
    return {
        "issues_scanned": len(issues),
        "pages_fetched": pages,
        "entries": entries,
        "findings": findings,
        "summary": {
            "delivery_entries": len(entries),
            "findings": len(findings),
            "findings_by_type": dict(sorted(counts.items())),
        },
    }


def _cell(value):
    return str(value) if value not in (None, "") else "-"


def render_table(report):
    headers = [
        "KEY", "STATUS", "REPO", "BASE REF", "DELIVERY REF", "BASE SHA",
        "DELIVERY SHA", "PRIMARY MR", "STACK PARENT", "FLAGS",
    ]
    findings_by_issue = defaultdict(set)
    for finding in report["findings"]:
        for key in finding.get("issue_keys", []):
            findings_by_issue[key].add(finding["type"])

    rows = []
    for entry in report["entries"]:
        rows.append([
            entry["key"], entry["status"], _cell(entry["repo_resource_id"]),
            _cell(entry["base_ref"]), _cell(entry["delivery_ref"]),
            _cell(entry["base_sha"]), _cell(entry["delivery_sha"]),
            _cell(entry["primary_mr_url"]), _cell(entry["stack_parent_issue"]),
            ",".join(sorted(findings_by_issue[entry["key"]])) or "-",
        ])

    print("Delivery ledger: %d entries from %d issues (%d pages)" % (
        len(rows), report["issues_scanned"], report["pages_fetched"]))
    if rows:
        widths = [len(header) for header in headers]
        for row in rows:
            widths = [max(width, len(value)) for width, value in zip(widths, row)]
        print("  ".join(header.ljust(width) for header, width in zip(headers, widths)))
        print("  ".join("-" * width for width in widths))
        for row in rows:
            print("  ".join(value.ljust(width) for value, width in zip(row, widths)))
    else:
        print("No issue carries delivery metadata.")

    print("\nFindings: %d" % len(report["findings"]))
    if report["findings"]:
        for finding in report["findings"]:
            print("- [%s] %s" % (finding["type"], finding["message"]))
    else:
        print("None.")


def parse_args(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--json", action="store_true", dest="as_json",
                        help="print the machine-readable report")
    return parser.parse_args(argv)


def main(argv=None):
    args = parse_args(argv)
    try:
        issues, pages = fetch_all_issues()
        report = make_report(issues, pages)
    except ReportError as exc:
        print("issue-deliveries: %s" % exc, file=sys.stderr)
        return 1

    if args.as_json:
        print(json.dumps(report, ensure_ascii=False, indent=2, sort_keys=True))
    else:
        render_table(report)
    return 0


if __name__ == "__main__":
    sys.exit(main())
