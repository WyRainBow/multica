# `multica issue pull-requests` response fields

Field dictionary for the elements of `{"pull_requests": [...]}`. SKILL.md keeps
the three questions a run actually asks; this is the rest.

- `number`, `html_url`, `title`
- `state` — the PR lifecycle as a **single enum**: `merged`, `closed`, `draft`,
  `open`. There is no separate `draft` or `merged` boolean in the response; the
  server folds them into `state` (merged wins, then closed, then draft, else
  open).
- `merged_at` — non-null once merged; a second confirmation of `state: merged`.
- `provider` — `github`, `forgejo`, `gitea`, or `gitlab`.
- `mergeable_state` — mirrors GitHub (`clean` / `dirty` surfaced; other values
  round-trip as unknown; retained for compatibility).
- GitHub API snapshot fields: `snapshot_available`, `mergeable`,
  `merge_state_status`, `checks_rollup`, `checks_total`, `checks_passed`,
  `checks_failed`, `checks_running`, `failed_check_names`,
  `snapshot_fetched_at`, `snapshot_stale`.

  `snapshot_available == true` means the feature is enabled AND the snapshot
  matches the PR's current head. Only then does `checks_rollup == null` mean
  "no checks". False means the snapshot feature is disabled, has not fetched
  yet, or only holds an older head — not that the PR has no CI.
- `checks_conclusion` — coarse CI compatibility status: `passed`, `failed`,
  `pending`, or `null`. GitHub derives it from the current API snapshot;
  Forgejo / Gitea / GitLab derive it from webhook commit statuses. Backed by
  the provider-appropriate check counts.
