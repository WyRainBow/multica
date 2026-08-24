"use client";

import { useMemo, useState } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { Lightbulb, Plus } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { cardListOptions } from "@multica/core/docs/queries";
import {
  issueDetailOptions,
  issueListOptions,
} from "@multica/core/issues/queries";
import type { Issue, Card } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "../../i18n";
import { useNavigation, useRowLink } from "../../navigation";
import { DocEditorDialog } from "./doc-editor-dialog";
import { DocItem } from "./doc-item";
import { groupCardsByDay } from "../group-by-day";
import { buildDocTree, filterDocsByPath } from "../doc-tree";
import { DocTreeNav } from "./doc-tree-nav";

/**
 * Everything a workspace has learned, newest first.
 *
 * A flat reverse-chronological list rather than a board or a tree: a card is
 * finished the moment it is written, so there is no state to move it through,
 * and grouping by the requirement it came from would bury the ones that came
 * from reading or from an incident.
 */
export function DocsPage({
  hideKinds,
  title,
  newKindPrefix,
}: {
  /** Exclude docs whose kind matches. The two wikis are the same page with
   *  opposite filters: this one hides the AgentWiki/ prefix, the Agent wiki
   *  hides everything else. Called with "" for an unfiled document. */
  hideKinds?: (kind: string) => boolean;
  /** Overrides the heading. The Agent wiki is this page under another name. */
  title?: string;
  /** Prefills the kind when creating here, so a page made on the Agent wiki
   *  tab is not filed somewhere that tab cannot show. */
  newKindPrefix?: string;
}) {
  const { t } = useT("docs");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const rowLink = useRowLink();
  const [search, setSearch] = useState("");
  // "" is 全部. Stored rather than derived because selecting a tab that then
  // empties (its last card was recategorised) must keep showing that tab's
  // empty state instead of silently jumping back to 全部.
  const [kind, setKind] = useState("");
  const [editing, setEditing] = useState<Card | null>(null);
  const [creating, setCreating] = useState(false);

  const { data, isLoading } = useQuery(cardListOptions(wsId));
  // The requirement a card points at is rendered as its identifier, so the
  // list needs the issues it references. One workspace query rather than one
  // per card.
  const { data: issues = [] } = useQuery(issueListOptions(wsId));
  const listedById = useMemo(() => {
    const map = new Map<string, Issue>();
    for (const issue of issues) map.set(issue.id, issue);
    return map;
  }, [issues]);

  // The workspace list is the FIRST PAGES only, so a document pointing at a
  // finished issue — the ones most often written about — found nothing there
  // and the row said the issue was unavailable. It was not: it was just past
  // the page. Anything the list did not cover is fetched by id.
  //
  // One query per unresolved document rather than per document: on a workspace
  // whose issues all fit in the first pages this fires zero times, and Query
  // dedupes two documents naming the same issue.
  const unresolvedIds = useMemo(() => {
    const ids = new Set<string>();
    for (const card of data?.cards ?? []) {
      if (card.issue_id && !listedById.has(card.issue_id))
        ids.add(card.issue_id);
    }
    return [...ids];
  }, [data?.cards, listedById]);

  const fetchedIssues = useQueries({
    queries: unresolvedIds.map((issueId) => issueDetailOptions(wsId, issueId)),
  });

  const issuesById = useMemo(() => {
    const map = new Map(listedById);
    for (const result of fetchedIssues) {
      if (result.data) map.set(result.data.id, result.data);
    }
    return map;
  }, [listedById, fetchedIssues]);

  // An issue that is genuinely gone, as opposed to one still being fetched.
  // Saying "unavailable" while the answer is in flight is the same wrong
  // message, a second later.
  const goneIssueIds = useMemo(() => {
    const gone = new Set<string>();
    unresolvedIds.forEach((issueId, index) => {
      if (fetchedIssues[index]?.isError) gone.add(issueId);
    });
    return gone;
  }, [unresolvedIds, fetchedIssues]);

  const cards = useMemo(
    () => (data?.cards ?? []).filter((c) => !hideKinds?.(c.kind ?? "")),
    [data?.cards, hideKinds],
  );
  // Tabs come from every card, not from the search result: a tab that
  // disappeared because the current query matched nothing in it would make
  // the category look deleted.
  const tree = useMemo(() => buildDocTree(cards), [cards]);
  const filtered = useMemo(() => {
    const inTab = filterDocsByPath(cards, kind);
    const query = search.trim().toLowerCase();
    if (!query) return inTab;
    return inTab.filter(
      (card) =>
        card.title.toLowerCase().includes(query) ||
        card.content.toLowerCase().includes(query),
    );
  }, [cards, kind, search]);

  const groups = useMemo(() => groupCardsByDay(filtered), [filtered]);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-2 border-b px-4 py-3">
        <h1 className="text-title-sm font-medium">{title ?? t(($) => $.page.title)}</h1>
        <span className="text-caption text-muted-foreground">
          {t(($) => $.page.count, { count: cards.length })}
        </span>
        <div className="ml-auto flex items-center gap-2">
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t(($) => $.page.search_placeholder)}
            className="h-8 w-56 text-body"
          />
          <Button size="sm" onClick={() => setCreating(true)}>
            <Plus className="size-3.5" />
            {t(($) => $.page.new)}
          </Button>
        </div>
      </div>

      <div className="flex min-h-0 flex-1">
        {/* Only once something has been filed. A lone "all" row is a control
            that cannot do anything, and it would sit there on every fresh
            workspace. */}
        {tree.length > 0 && (
          <DocTreeNav
            tree={tree}
            total={cards.length}
            selected={kind}
            onSelect={setKind}
            className="w-52 shrink-0 overflow-y-auto border-r p-2"
          />
        )}

        <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
          {isLoading ? null : filtered.length === 0 ? (
            <EmptyState
              hasCards={cards.length > 0}
              onCreate={() => setCreating(true)}
            />
          ) : (
            // One column, not a grid. A document is prose read one at a time, and
            // the day headings only mean anything if the reading order is a
            // single line down the page.
            //
            // Left-aligned, not centred. Centring suits a page that is only ever
            // read; this one is a working list you scan and come back to, and a
            // column floating in the middle puts it somewhere the eye has to go
            // looking for. The width cap stays — prose past ~80 characters a line
            // is harder to read, and that is true wherever the column sits.
            <div className="w-full max-w-3xl">
              {groups.map((group) => (
                <section key={group.day}>
                  <DayHeading date={group.date} count={group.cards.length} />
                  {group.cards.map((card) => (
                    <TimelineRow key={card.id} at={card.created_at}>
                      <DocItem
                        card={card}
                        issue={
                          card.issue_id
                            ? issuesById.get(card.issue_id)
                            : undefined
                        }
                        issueGone={
                          !!card.issue_id && goneIssueIds.has(card.issue_id)
                        }
                        docLink={rowLink(paths.docDetail(card.id))}
                        onEdit={() => navigation.push(paths.docDetail(card.id))}
                        onOpenIssue={(identifier) =>
                          navigation.push(paths.issueDetail(identifier))
                        }
                      />
                    </TimelineRow>
                  ))}
                </section>
              ))}
            </div>
          )}
        </div>
      </div>

      {(creating || editing) && (
        <DocEditorDialog
          card={editing}
          defaultKind={newKindPrefix}
          onClose={() => {
            setCreating(false);
            setEditing(null);
          }}
        />
      )}
    </div>
  );
}

/**
 * The day a run of cards belongs to, plus how many there were.
 *
 * Sticky so the day stays named while its cards scroll past — a long day
 * otherwise leaves the reader looking at undated rows. `top-0` works because
 * the scroll container is the ancestor, not the window.
 */
function DayHeading({ date, count }: { date: Date; count: number }) {
  const { t, i18n } = useT("docs");
  // Formatted with the UI language rather than the browser locale: the rest of
  // the page is already translated, and a Chinese page with an English weekday
  // reads as a bug.
  const day = new Intl.DateTimeFormat(i18n.language, {
    month: "long",
    day: "numeric",
  }).format(date);
  const weekday = new Intl.DateTimeFormat(i18n.language, {
    weekday: "long",
  }).format(date);

  return (
    <div className="sticky top-0 z-10 -mx-1 flex items-baseline gap-2 bg-background/95 px-1 py-3 backdrop-blur">
      <h2 className="text-title-sm font-semibold">{day}</h2>
      <span className="text-caption text-muted-foreground">{weekday}</span>
      <span className="text-caption text-muted-foreground">·</span>
      <span className="text-caption text-muted-foreground">
        {t(($) => $.page.day_count, { count })}
      </span>
    </div>
  );
}

/**
 * One row of the timeline: the time on the left, then the rail, then the card.
 *
 * The rail is drawn by the row rather than by a single line behind the list,
 * so it cannot fall out of step with rows of different heights. It runs the
 * row's full height and the dot is positioned against the card's first line,
 * which is what makes a column of dots line up with a column of titles.
 */
function TimelineRow({
  at,
  children,
}: {
  at: string;
  children: React.ReactNode;
}) {
  const { i18n } = useT("docs");
  const time = new Intl.DateTimeFormat(i18n.language, {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(new Date(at));

  return (
    <div className="flex gap-3 pb-4">
      <time className="w-12 shrink-0 pt-5 text-right text-caption tabular-nums text-muted-foreground">
        {time}
      </time>
      <div className="relative w-3 shrink-0" aria-hidden>
        <span className="absolute left-1/2 top-0 h-full w-px -translate-x-1/2 bg-border" />
        <span className="absolute left-1/2 top-5 size-1.5 -translate-x-1/2 rounded-full bg-muted-foreground/60 ring-4 ring-background" />
      </div>
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}

/**
 * Two different empty states. "Nothing written yet" invites the first card;
 * "nothing matches" must not, or the button silently discards the search the
 * user just typed.
 */
function EmptyState({
  hasCards,
  onCreate,
}: {
  hasCards: boolean;
  onCreate: () => void;
}) {
  const { t } = useT("docs");
  return (
    <div className="flex flex-col items-center justify-center gap-3 py-24 text-center">
      <Lightbulb className="size-8 text-faint-foreground" />
      <p className="text-body text-muted-foreground">
        {hasCards ? t(($) => $.page.no_matches) : t(($) => $.page.empty)}
      </p>
      {!hasCards && (
        <Button size="sm" variant="outline" onClick={onCreate}>
          <Plus className="size-3.5" />
          {t(($) => $.page.new)}
        </Button>
      )}
    </div>
  );
}
