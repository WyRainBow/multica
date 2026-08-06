"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Lightbulb, Plus } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { cardListOptions } from "@multica/core/cards/queries";
import { issueListOptions } from "@multica/core/issues/queries";
import type { Issue, Card } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";
import { CardEditorDialog } from "./card-editor-dialog";
import { CardItem } from "./card-item";
import { cn } from "@multica/ui/lib/utils";
import { groupCardsByDay } from "../group-by-day";
import { cardKindTabs, filterCardsByKind } from "../card-kinds";

/**
 * Everything a workspace has learned, newest first.
 *
 * A flat reverse-chronological list rather than a board or a tree: a card is
 * finished the moment it is written, so there is no state to move it through,
 * and grouping by the requirement it came from would bury the ones that came
 * from reading or from an incident.
 */
/**
 * One tab. Underline plus weight, not a background: the row sits directly on
 * the page and a filled pill would read as a button that does something.
 *
 * The active state is carried by font weight and text colour — dimensions
 * hover does not touch — so hovering the selected tab cannot visually
 * downgrade it to look merely hovered.
 */
function KindTab({
  label,
  count,
  active,
  onSelect,
}: {
  label: string;
  count: number;
  active: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-pressed={active}
      className={cn(
        "-mb-px flex shrink-0 items-center gap-1.5 border-b-2 px-3 py-2 text-body transition-colors",
        active
          ? "border-primary font-medium text-foreground"
          : "border-transparent text-muted-foreground hover:text-foreground",
      )}
    >
      {label}
      <span className="text-caption tabular-nums text-faint-foreground">{count}</span>
    </button>
  );
}

export function CardsPage() {
  const { t } = useT("cards");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
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
  const issuesById = useMemo(() => {
    const map = new Map<string, Issue>();
    for (const issue of issues) map.set(issue.id, issue);
    return map;
  }, [issues]);

  const cards = data?.cards ?? [];
  // Tabs come from every card, not from the search result: a tab that
  // disappeared because the current query matched nothing in it would make
  // the category look deleted.
  const tabs = useMemo(() => cardKindTabs(cards), [cards]);
  const filtered = useMemo(() => {
    const inTab = filterCardsByKind(cards, kind);
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
        <h1 className="text-title-sm font-medium">{t(($) => $.page.title)}</h1>
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

      {/* Only once something has been filed. A lone 全部 tab is a control that
          cannot do anything, and it would sit there on every fresh workspace. */}
      {tabs.length > 0 && (
        <div className="flex items-center gap-1 overflow-x-auto border-b px-4">
          <KindTab
            label={t(($) => $.page.all_kinds)}
            count={cards.length}
            active={kind === ""}
            onSelect={() => setKind("")}
          />
          {tabs.map((tab) => (
            <KindTab
              key={tab.kind}
              label={tab.kind}
              count={tab.count}
              active={kind === tab.kind}
              onSelect={() => setKind(tab.kind)}
            />
          ))}
        </div>
      )}

      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
        {isLoading ? null : filtered.length === 0 ? (
          <EmptyState
            hasCards={cards.length > 0}
            onCreate={() => setCreating(true)}
          />
        ) : (
          // One column, not a grid. A card is prose read one at a time, and
          // the day headings only mean anything if the reading order is a
          // single line down the page.
          <div className="mx-auto w-full max-w-3xl">
            {groups.map((group) => (
              <section key={group.day}>
                <DayHeading date={group.date} count={group.cards.length} />
                {group.cards.map((card) => (
                  <TimelineRow key={card.id} at={card.created_at}>
                    <CardItem
                      card={card}
                      issue={
                        card.issue_id ? issuesById.get(card.issue_id) : undefined
                      }
                      onEdit={() => setEditing(card)}
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

      {(creating || editing) && (
        <CardEditorDialog
          card={editing}
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
  const { t, i18n } = useT("cards");
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
  const { i18n } = useT("cards");
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
  const { t } = useT("cards");
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
