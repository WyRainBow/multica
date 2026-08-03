#!/usr/bin/env python3
"""Copy issues, comments and metadata from one Multica workspace into another.

Both sides are addressed through `multica` CLI profiles, so this works for
cloud -> self-hosted, self-hosted -> self-hosted, or any other pair the CLI can
authenticate against.

This is a one-shot copy, not a continuous sync. Re-running is safe: every
created entity is recorded in the state file and skipped next time, so an
interrupted run resumes where it stopped.

What cannot be carried over, because the API has no write path for it:

  - issue identifiers and numbers (the target assigns its own)
  - created_at / updated_at (everything lands with the import timestamp)
  - comment authorship and post time -- preserved as a quoted header on each
    migrated comment instead
  - attachments

Assignees are remapped by matching workspace members on email and agents on
name. Anything that finds no counterpart in the target is left unassigned and
reported at the end.

Usage:
    scripts/migrate-issues.py export --from cloud
    scripts/migrate-issues.py plan   --from cloud --to local
    scripts/migrate-issues.py run    --from cloud --to local
    scripts/migrate-issues.py verify --from cloud --to local

Omit --from / --to to use the CLI's default profile on that side.
"""

import argparse
import json
import os
import subprocess
import sys
from datetime import datetime, timezone

DEFAULT_DATA_DIR = os.path.join(
    os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
    ".multica", "issue-migration",
)

ATTRIBUTION = "> 迁移自 %s · 原作者 %s · 原发表时间 %s\n\n"


class Migration:
    def __init__(self, source, target, data_dir):
        self.source = source
        self.target = target
        self.data_dir = data_dir
        self.issues_file = os.path.join(data_dir, "issues.json")
        self.comments_file = os.path.join(data_dir, "comments.json")
        self.projects_file = os.path.join(data_dir, "projects.json")
        self.state_file = os.path.join(data_dir, "state.json")
        self.state = self._load_state()
        self._principals = None

    # -- CLI plumbing -----------------------------------------------------

    def _run(self, profile, args, stdin=None):
        cmd = ["multica"]
        if profile:
            cmd += ["--profile", profile]
        cmd += args
        proc = subprocess.run(cmd, input=stdin, capture_output=True, text=True)
        if proc.returncode != 0:
            raise RuntimeError(
                "command failed: %s\nstdout: %s\nstderr: %s"
                % (" ".join(cmd), proc.stdout.strip(), proc.stderr.strip())
            )
        return proc.stdout

    def from_source(self, args, stdin=None):
        return json.loads(self._run(self.source, args, stdin))

    def to_target(self, args, stdin=None):
        return self._run(self.target, args, stdin)

    def to_target_json(self, args, stdin=None):
        return json.loads(self.to_target(args, stdin))

    # -- state ------------------------------------------------------------

    def _load_state(self):
        if os.path.exists(self.state_file):
            with open(self.state_file) as fh:
                return json.load(fh)
        return {"projects": {}, "issues": {}, "comments": {},
                "resolved": [], "metadata": []}

    def save_state(self):
        os.makedirs(self.data_dir, exist_ok=True)
        with open(self.state_file, "w") as fh:
            json.dump(self.state, fh, ensure_ascii=False, indent=2)

    def ref(self, bucket, key, dry_run=False):
        """Resolve a source id to the id it was created under in the target."""
        entry = self.state[bucket].get(key)
        if entry is None:
            if dry_run:
                return "<pending>"
            raise KeyError("%s/%s has not been migrated yet" % (bucket, key))
        return entry["id"] if isinstance(entry, dict) else entry

    # -- principal mapping ------------------------------------------------

    def principals(self):
        """Map source assignee/author ids to display name and target assignee.

        Members are keyed by user_id because that is what issue.assignee_id
        and comment.author_id carry for a member. Agents are keyed by agent id.
        """
        if self._principals is not None:
            return self._principals

        src_members = self.from_source(["workspace", "member", "list", "--output", "json"])
        dst_members = json.loads(self.to_target(
            ["workspace", "member", "list", "--output", "json"]))
        by_email = {m.get("email"): m for m in dst_members if m.get("email")}

        mapping = {}
        for member in src_members:
            match = by_email.get(member.get("email"))
            mapping[("member", member.get("user_id"))] = {
                "name": member.get("name") or member.get("email"),
                "assignee": match.get("name") if match else None,
            }

        src_agents = self.from_source(["agent", "list", "--output", "json"])
        dst_agents = json.loads(self.to_target(["agent", "list", "--output", "json"]))
        by_name = {(a.get("name") or "").lower(): a for a in dst_agents}
        for agent in src_agents:
            match = by_name.get((agent.get("name") or "").lower())
            mapping[("agent", agent.get("id"))] = {
                "name": agent.get("name"),
                "assignee": match.get("name") if match else None,
            }

        self._principals = mapping
        return mapping

    def label(self, kind, ident):
        entry = self.principals().get((kind, ident))
        if entry is None:
            return "%s %s" % (kind, (ident or "unknown")[:8])
        return "%s（%s）" % (entry["name"], kind)

    def assignee_name(self, kind, ident):
        entry = self.principals().get((kind, ident))
        return entry["assignee"] if entry else None


# -- helpers --------------------------------------------------------------


def fmt_time(raw):
    """Render an API timestamp as `YYYY-MM-DD HH:MM:SS UTC`."""
    if not raw:
        return "时间未知"
    try:
        moment = datetime.fromisoformat(raw.replace("Z", "+00:00"))
    except ValueError:
        return raw
    if moment.tzinfo is None:
        moment = moment.replace(tzinfo=timezone.utc)
    return moment.astimezone(timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")


def day(value):
    """The API returns dates as timestamps; the CLI date flags want YYYY-MM-DD."""
    return value[:10] if value else None


def as_list(payload, key):
    return payload if isinstance(payload, list) else payload.get(key, [])


def topo_sorted(issues):
    """Parents before children, so --parent can be passed at creation time."""
    by_id = {i["id"]: i for i in issues}
    ordered, seen = [], set()

    def visit(issue):
        if issue["id"] in seen:
            return
        seen.add(issue["id"])
        parent = by_id.get(issue.get("parent_issue_id"))
        if parent is not None:
            visit(parent)
        ordered.append(issue)

    for issue in issues:
        visit(issue)
    return ordered


def comments_ordered(items):
    """Thread roots before replies, otherwise oldest first."""
    by_id = {c["id"]: c for c in items}
    ordered, seen = [], set()

    def visit(comment):
        if comment["id"] in seen:
            return
        seen.add(comment["id"])
        parent = by_id.get(comment.get("parent_id"))
        if parent is not None:
            visit(parent)
        ordered.append(comment)

    for comment in sorted(items, key=lambda c: c.get("created_at") or ""):
        visit(comment)
    return ordered


# -- commands -------------------------------------------------------------


def do_export(mig, limit):
    os.makedirs(mig.data_dir, exist_ok=True)

    issues = mig.from_source(["issue", "list", "--limit", str(limit), "--output", "json"])
    if issues.get("has_more"):
        raise SystemExit("source has more issues than --limit=%d" % limit)
    with open(mig.issues_file, "w") as fh:
        json.dump(issues, fh, ensure_ascii=False, indent=2)

    projects = as_list(mig.from_source(["project", "list", "--output", "json"]), "projects")
    with open(mig.projects_file, "w") as fh:
        json.dump(projects, fh, ensure_ascii=False, indent=2)

    comments = {}
    for issue in issues["issues"]:
        raw = mig.from_source(["issue", "comment", "list", issue["identifier"],
                               "--output", "json"])
        comments[issue["identifier"]] = as_list(raw, "comments")
    with open(mig.comments_file, "w") as fh:
        json.dump(comments, fh, ensure_ascii=False, indent=2)

    print("exported %d issues, %d projects, %d comments -> %s"
          % (len(issues["issues"]), len(projects),
             sum(len(v) for v in comments.values()), mig.data_dir))


def do_run(mig, dry_run):
    issues = json.load(open(mig.issues_file))["issues"]
    projects = json.load(open(mig.projects_file))
    comments = json.load(open(mig.comments_file))
    state = mig.state
    unmapped = []

    for project in projects:
        if project["id"] in state["projects"]:
            continue
        args = ["project", "create", "--title", project["title"], "--output", "json"]
        for flag, value in (("--description", project.get("description")),
                            ("--status", project.get("status")),
                            ("--icon", project.get("icon")),
                            ("--start-date", day(project.get("start_date"))),
                            ("--due-date", day(project.get("due_date")))):
            if value:
                args += [flag, value]
        print("project: %s" % project["title"])
        if dry_run:
            continue
        state["projects"][project["id"]] = mig.to_target_json(args)["id"]
        mig.save_state()

    for issue in topo_sorted(issues):
        if issue["id"] in state["issues"]:
            continue
        args = ["issue", "create", "--title", issue["title"], "--output", "json",
                "--description-stdin", "--allow-duplicate"]
        for flag, value in (("--status", issue.get("status")),
                            ("--priority", issue.get("priority")),
                            ("--start-date", day(issue.get("start_date"))),
                            ("--due-date", day(issue.get("due_date")))):
            if value:
                args += [flag, value]
        if issue.get("project_id"):
            args += ["--project", mig.ref("projects", issue["project_id"], dry_run)]
        if issue.get("parent_issue_id"):
            args += ["--parent", mig.ref("issues", issue["parent_issue_id"], dry_run)]
            if issue.get("stage"):
                args += ["--stage", str(issue["stage"])]
        if issue.get("assignee_id"):
            assignee = mig.assignee_name(issue.get("assignee_type"), issue["assignee_id"])
            if assignee:
                args += ["--assignee", assignee]
            else:
                unmapped.append("%s (%s)" % (issue["identifier"], issue.get("assignee_type")))

        print("issue: %s %s" % (issue["identifier"], issue["title"][:50]))
        if dry_run:
            continue
        created = mig.to_target_json(args, stdin=issue.get("description") or "")
        state["issues"][issue["id"]] = {"id": created["id"],
                                        "identifier": created["identifier"],
                                        "from": issue["identifier"]}
        mig.save_state()

    for issue in issues:
        for key, value in (issue.get("metadata") or {}).items():
            marker = "%s:%s" % (issue["id"], key)
            if marker in state["metadata"]:
                continue
            print("metadata: %s %s=%s" % (issue["identifier"], key, value))
            if dry_run:
                continue
            mig.to_target(["issue", "metadata", "set", mig.ref("issues", issue["id"]),
                           "--key", key, "--value", str(value), "--type", "string"])
            state["metadata"].append(marker)
            mig.save_state()

    for issue in issues:
        for comment in comments_ordered(comments.get(issue["identifier"], [])):
            if comment["id"] in state["comments"]:
                continue
            args = ["issue", "comment", "add", mig.ref("issues", issue["id"], dry_run),
                    "--content-stdin", "--output", "json"]
            if comment.get("parent_id"):
                args += ["--parent", mig.ref("comments", comment["parent_id"], dry_run)]
            header = ATTRIBUTION % (
                issue["identifier"],
                mig.label(comment.get("author_type"), comment.get("author_id")),
                fmt_time(comment.get("created_at")),
            )
            print("comment: %s %s" % (issue["identifier"], comment["id"][:8]))
            if dry_run:
                continue
            created = mig.to_target_json(args, stdin=header + (comment.get("content") or ""))
            state["comments"][comment["id"]] = created["id"]
            mig.save_state()

    for issue in issues:
        for comment in comments.get(issue["identifier"], []):
            if not comment.get("resolved_at") or comment["id"] in state["resolved"]:
                continue
            print("resolve: %s" % comment["id"][:8])
            if dry_run:
                continue
            mig.to_target(["issue", "comment", "resolve",
                           mig.ref("comments", comment["id"])])
            state["resolved"].append(comment["id"])
            mig.save_state()

    if unmapped:
        print("\nleft unassigned (no counterpart in target): %s" % ", ".join(unmapped))
    print("\ndone: %d issues, %d comments" % (len(state["issues"]), len(state["comments"])))


def do_verify(mig):
    issues = json.load(open(mig.issues_file))["issues"]
    comments = json.load(open(mig.comments_file))
    state = mig.state
    problems = []

    target = mig.to_target_json(["issue", "list", "--limit", "500", "--output", "json"])
    target_by_id = {i["id"]: i for i in target["issues"]}
    print("target issues: %d (source: %d)" % (target["total"], len(issues)))

    for issue in issues:
        mapped = state["issues"].get(issue["id"])
        if mapped is None:
            problems.append("%s not migrated" % issue["identifier"])
            continue
        got = target_by_id.get(mapped["id"])
        if got is None:
            problems.append("%s -> %s missing in target"
                            % (issue["identifier"], mapped["identifier"]))
            continue
        for field in ("title", "status", "priority", "description"):
            if (issue.get(field) or "") != (got.get(field) or ""):
                problems.append("%s %s differs" % (issue["identifier"], field))
        parent = issue.get("parent_issue_id")
        parent = state["issues"][parent]["id"] if parent else None
        if parent != got.get("parent_issue_id"):
            problems.append("%s parent differs" % issue["identifier"])
        if (issue.get("metadata") or {}) != (got.get("metadata") or {}):
            problems.append("%s metadata differs" % issue["identifier"])

    total_target = 0
    for issue in issues:
        mapped = state["issues"].get(issue["id"])
        if mapped is None:
            continue
        got = as_list(mig.to_target_json(
            ["issue", "comment", "list", mapped["id"], "--output", "json"]), "comments")
        total_target += len(got)
        want = len(comments.get(issue["identifier"], []))
        if want != len(got):
            problems.append("%s has %d comments, expected %d"
                            % (issue["identifier"], len(got), want))
    print("target comments: %d (source: %d)"
          % (total_target, sum(len(v) for v in comments.values())))

    print("\nmapping:")
    for issue in issues:
        mapped = state["issues"].get(issue["id"])
        if mapped:
            print("  %-8s -> %-8s %s" % (issue["identifier"], mapped["identifier"],
                                         issue["title"][:44]))

    if problems:
        print("\nPROBLEMS (%d):" % len(problems))
        for line in problems:
            print("  - %s" % line)
        sys.exit(1)
    print("\nall checks passed")


def main():
    parser = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("command", choices=["export", "plan", "run", "verify"])
    parser.add_argument("--from", dest="source", default=None,
                        help="source CLI profile (default: the CLI default profile)")
    parser.add_argument("--to", dest="target", default=None,
                        help="target CLI profile (default: the CLI default profile)")
    parser.add_argument("--data-dir", default=DEFAULT_DATA_DIR,
                        help="where the export and state files live (default: %(default)s)")
    parser.add_argument("--limit", type=int, default=500,
                        help="max issues to export (default: %(default)s)")
    args = parser.parse_args()

    if args.command != "export" and args.source == args.target:
        parser.error("--from and --to resolve to the same profile")

    mig = Migration(args.source, args.target, args.data_dir)
    if args.command == "export":
        do_export(mig, args.limit)
    elif args.command == "plan":
        do_run(mig, dry_run=True)
    elif args.command == "run":
        do_run(mig, dry_run=False)
    else:
        do_verify(mig)


if __name__ == "__main__":
    main()
