#!/usr/bin/env python3

import unittest

import issue_deliveries


def issue(key, metadata=None, issue_id=None):
    return {
        "id": issue_id or (key.lower() + "-id"),
        "identifier": key,
        "title": key + " title",
        "status": "in_progress",
        "metadata": metadata or {},
    }


class FetchAllIssuesTest(unittest.TestCase):
    def test_empty_workspace_is_a_valid_single_page(self):
        issues, page_count = issue_deliveries.fetch_all_issues(
            lambda _args: {"issues": [], "offset": 0, "limit": 50,
                           "has_more": False})

        self.assertEqual(issues, [])
        self.assertEqual(page_count, 1)

    def test_follows_response_pagination_until_has_more_is_false(self):
        calls = []
        pages = {
            0: {"issues": [issue("COC-1"), issue("COC-2")],
                "offset": 0, "limit": 2, "has_more": True},
            2: {"issues": [issue("COC-3")],
                "offset": 2, "limit": 2, "has_more": False},
        }

        def fake_run(args):
            calls.append(args)
            offset = int(args[args.index("--offset") + 1])
            return pages[offset]

        issues, page_count = issue_deliveries.fetch_all_issues(fake_run, page_limit=2)

        self.assertEqual([item["identifier"] for item in issues],
                         ["COC-1", "COC-2", "COC-3"])
        self.assertEqual(page_count, 2)
        self.assertEqual([call[call.index("--offset") + 1] for call in calls],
                         ["0", "2"])

    def test_rejects_a_has_more_page_that_cannot_advance(self):
        with self.assertRaises(issue_deliveries.ReportError):
            issue_deliveries.fetch_all_issues(
                lambda _args: {"issues": [], "offset": 0, "limit": 50,
                               "has_more": True})


class LedgerTest(unittest.TestCase):
    def test_ignores_issues_without_delivery_metadata(self):
        report = issue_deliveries.make_report([issue("COC-1")], pages=1)

        self.assertEqual(report["entries"], [])
        self.assertEqual(report["findings"], [])

    def test_prefers_current_keys_and_marks_legacy_keys_deprecated(self):
        entries = issue_deliveries.build_entries([
            issue("COC-10", {
                "git.base_ref": "main",
                "git.delivery_ref": "feature/current",
                "baseline_ref": "main",
                "delivery_branch": "feature/legacy",
            }),
        ])

        self.assertEqual(entries[0]["base_ref"], "main")
        self.assertEqual(entries[0]["delivery_ref"], "feature/current")
        self.assertEqual(entries[0]["deprecated_keys"],
                         ["baseline_ref", "delivery_branch"])
        self.assertEqual(entries[0]["metadata_conflicts"][0]["field"],
                         "delivery_ref")

    def test_reports_missing_refs_duplicate_ref_and_unconfirmed_mr(self):
        entries = issue_deliveries.build_entries([
            issue("COC-1", {"git.delivery_ref": "feature/shared"}),
            issue("COC-2", {
                "git.base_ref": "main",
                "git.delivery_ref": "feature/shared",
                "vcs.primary_mr_url": "https://git.example/mr/2",
            }),
            issue("COC-3", {"git.base_ref": "main"}),
        ])

        findings = issue_deliveries.analyze(entries)
        finding_types = [finding["type"] for finding in findings]

        self.assertIn("missing_base_ref", finding_types)
        self.assertIn("missing_delivery_ref", finding_types)
        self.assertIn("duplicate_delivery_ref", finding_types)
        self.assertIn("unconfirmed_mr", finding_types)

    def test_boolean_true_confirms_a_primary_mr(self):
        entries = issue_deliveries.build_entries([
            issue("COC-1", {
                "git.base_ref": "main",
                "git.delivery_ref": "feature/one",
                "vcs.primary_mr_url": "https://git.example/mr/1",
                "vcs.primary_mr_confirmed": True,
            }),
        ])

        self.assertNotIn("unconfirmed_mr",
                         [item["type"] for item in issue_deliveries.analyze(entries)])

    def test_finds_each_stack_cycle_once_using_keys_or_ids(self):
        entries = issue_deliveries.build_entries([
            issue("COC-1", {"git.base_ref": "main", "git.delivery_ref": "one",
                            "git.stack_parent_issue": "COC-2"}, "uuid-one"),
            issue("COC-2", {"git.base_ref": "one", "git.delivery_ref": "two",
                            "git.stack_parent_issue": "uuid-one"}, "uuid-two"),
            issue("COC-3", {"git.base_ref": "main", "git.delivery_ref": "three",
                            "git.stack_parent_issue": "COC-1"}, "uuid-three"),
        ])

        self.assertEqual(issue_deliveries.find_stack_cycles(entries),
                         [["COC-1", "COC-2", "COC-1"]])


if __name__ == "__main__":
    unittest.main()
