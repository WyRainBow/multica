"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueHistoryOptions } from "@multica/core/issues/queries";
import type {
  IssueHistoryDecisionRow,
  IssueHistoryDocument,
  IssueHistoryRound,
} from "@multica/core/types";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";
import { useWorkspacePaths } from "@multica/core/paths";

/**
 * The card's history, in the three shapes it actually has.
 *
 * Three tabs rather than one merged timeline (COC-328 D2). Decisions, review
 * rounds and documents are three kinds of object with two separate derivations:
 * a decision's status comes only from other decisions, a round's verdict only
 * from the round document. Merged, the reader has to work out which system each
 * line belongs to, and the field sets overlap so little that most columns would
 * be empty in most rows.
 */
type HistoryTab = "decisions" | "rounds" | "documents";

export function IssueHistorySection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const [tab, setTab] = useState<HistoryTab>("decisions");

  const { data } = useQuery(issueHistoryOptions(wsId, issueId));
  const decisions = data?.decisions ?? [];
  const rounds = data?.rounds ?? [];
  const documents = data?.documents ?? [];

  // A card with no history at all gets no heading. Most cards never grow one,
  // and an empty section under every card is noise on the page that matters.
  if (decisions.length === 0 && rounds.length === 0 && documents.length === 0) {
    return null;
  }

  const tabs: { key: HistoryTab; label: string; count: number }[] = [
    { key: "decisions", label: t(($) => $.history.tab_decisions), count: decisions.length },
    { key: "rounds", label: t(($) => $.history.tab_rounds), count: rounds.length },
    { key: "documents", label: t(($) => $.history.tab_documents), count: documents.length },
  ];

  return (
    <section className="mt-8">
      <h2 className="text-title font-medium">{t(($) => $.history.title)}</h2>
      <p className="mt-1 text-caption text-muted-foreground">
        {t(($) => $.history.subtitle)}
      </p>

      <div className="mt-3 flex flex-wrap gap-1 border-b border-border">
        {tabs.map((item) => (
          <button
            key={item.key}
            type="button"
            onClick={() => setTab(item.key)}
            data-active={tab === item.key ? "true" : undefined}
            /* The active tab keeps its weight and colour while hovered; hover
               only moves the background, so a selected tab never visually
               downgrades to a plain hover. */
            className="-mb-px border-b-2 border-transparent px-3 py-1.5 text-caption text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground data-active:border-foreground data-active:font-medium data-active:text-foreground"
          >
            {item.label}
            <span className="ml-1.5 tabular-nums text-muted-foreground">
              {item.count}
            </span>
          </button>
        ))}
      </div>

      <div className="mt-3">
        {tab === "decisions" && <DecisionTable rows={decisions} />}
        {tab === "rounds" && <RoundList rounds={rounds} />}
        {tab === "documents" && <DocumentList documents={documents} />}
      </div>
    </section>
  );
}

/**
 * The decision table: one row per decision or open question, never a detail
 * row.
 *
 * Open questions first — an unanswered one is the only row that can block the
 * next run. After that plain descending number rather than grouping by status,
 * so a superseded decision sits directly under the one that replaced it. That
 * pair is the relationship this table exists to show, and gaps keep their slot
 * in the same sequence, which is what makes them read as gaps.
 */
function DecisionTable({ rows }: { rows: IssueHistoryDecisionRow[] }) {
  const { t } = useT("issues");
  const live = rows.filter((row) => row.status !== "legacy");
  const legacy = rows.filter((row) => row.status === "legacy");

  return (
    <div className="flex flex-col gap-4">
      {live.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[720px] border-collapse text-caption">
            <thead>
              <tr className="border-b border-border text-left text-muted-foreground">
                <th className="py-1.5 pr-3 font-medium">{t(($) => $.history.col_id)}</th>
                <th className="py-1.5 pr-3 font-medium">{t(($) => $.history.col_status)}</th>
                <th className="py-1.5 pr-3 font-medium">{t(($) => $.history.col_question)}</th>
                <th className="py-1.5 pr-3 font-medium">{t(($) => $.history.col_decision)}</th>
                <th className="py-1.5 pr-3 font-medium">{t(($) => $.history.col_decided_by)}</th>
                <th className="py-1.5 font-medium">{t(($) => $.history.col_decided_at)}</th>
              </tr>
            </thead>
            <tbody>
              {live.map((row) => (
                <DecisionRow key={row.id + row.status} row={row} />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Legacy cards sit in their own block, not in the table: they have no
        derived status, and giving them a row under a status column would
        invite the reader to read the blank as "none". */}
      {legacy.length > 0 && (
        <div className="rounded-md border border-dashed border-border p-3">
          <p className="text-caption font-medium">
            {t(($) => $.history.legacy_title)}
          </p>
          <p className="mt-0.5 text-caption text-muted-foreground">
            {t(($) => $.history.legacy_note)}
          </p>
          <ul className="mt-2 flex flex-col gap-1">
            {legacy.map((row) => (
              <li key={row.doc_id} className="text-caption">
                <DocLink docId={row.doc_id} label={row.id} />
                <span className="ml-2 text-muted-foreground">{row.title}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function DecisionRow({ row }: { row: IssueHistoryDecisionRow }) {
  const { t } = useT("issues");

  if (row.status === "gap") {
    return (
      <tr className="border-b border-border/60 text-muted-foreground">
        <td className="py-1.5 pr-3 font-mono">{row.id}</td>
        <td className="py-1.5 pr-3">{t(($) => $.history.status_gap)}</td>
        <td className="py-1.5 pr-3" colSpan={4}>
          {t(($) => $.history.gap_note)}
        </td>
      </tr>
    );
  }

  const open = row.status === "open";
  return (
    <tr className="border-b border-border/60 align-top">
      <td className="py-1.5 pr-3 font-mono">
        <DocLink docId={row.doc_id} label={row.id} />
      </td>
      <td className="py-1.5 pr-3">
        <StatusLabel row={row} />
      </td>
      <td className="py-1.5 pr-3">{row.question}</td>
      <td className="py-1.5 pr-3">
        {open ? (
          <span className="text-muted-foreground">
            {t(($) => $.history.undecided)}
          </span>
        ) : (
          row.summary
        )}
      </td>
      <td className="py-1.5 pr-3">{open ? "—" : row.decided_by || "—"}</td>
      <td className="py-1.5 whitespace-nowrap tabular-nums text-muted-foreground">
        {open ? t(($) => $.history.undecided) : formatDecidedAt(row.decided_at)}
      </td>
    </tr>
  );
}

function StatusLabel({ row }: { row: IssueHistoryDecisionRow }) {
  const { t } = useT("issues");
  if (row.status === "open") {
    return (
      <span className="rounded bg-amber-500/15 px-1.5 py-0.5 font-medium text-amber-700 dark:text-amber-400">
        {t(($) => $.history.status_open)}
      </span>
    );
  }
  if (row.status === "superseded") {
    return (
      <span className="text-muted-foreground">
        {row.superseded_by === ""
          ? t(($) => $.history.status_superseded)
          : t(($) => $.history.status_superseded_by, { id: row.superseded_by })}
      </span>
    );
  }
  if (row.status === "current") {
    return <span className="font-medium">{t(($) => $.history.status_current)}</span>;
  }
  // A status this build has not heard of renders as itself rather than
  // vanishing, which is the point of not typing it as an enum on read.
  return <span className="text-muted-foreground">{row.status}</span>;
}

/**
 * Decision time, to the minute.
 *
 * The stored value keeps its seconds and the full document shows them; the list
 * does not need them, because the list is already ordered and a reader
 * comparing two rows reads their order off the page rather than off the
 * timestamp. Seconds earn their place where a reader has to check a supersede
 * against a single card, which is the document, not this table (COC-328 D3).
 *
 * The UTC offset stays. The same wall-clock time means different things in
 * different places, and this table is read by people who move between them.
 */
function formatDecidedAt(iso: string): string {
  if (iso === "") return "—";
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return iso;

  const pad = (n: number) => String(n).padStart(2, "0");
  const offsetMinutes = -at.getTimezoneOffset();
  const sign = offsetMinutes < 0 ? "-" : "+";
  const abs = Math.abs(offsetMinutes);
  return (
    `${at.getFullYear()}-${pad(at.getMonth() + 1)}-${pad(at.getDate())} ` +
    `${pad(at.getHours())}:${pad(at.getMinutes())} ` +
    `UTC${sign}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`
  );
}

function RoundList({ rounds }: { rounds: IssueHistoryRound[] }) {
  const { t } = useT("issues");
  const live = rounds.filter((round) => round.legacy !== true);
  const legacy = rounds.filter((round) => round.legacy === true);

  return (
    <div className="flex flex-col gap-4">
      <ul className="flex flex-col gap-2">
        {live.map((round) => (
          <RoundRow key={round.doc_id} round={round} />
        ))}
      </ul>
      {legacy.length > 0 && (
        <div className="rounded-md border border-dashed border-border p-3">
          <p className="text-caption font-medium">
            {t(($) => $.history.rounds_legacy_title)}
          </p>
          <p className="mt-0.5 text-caption text-muted-foreground">
            {t(($) => $.history.rounds_legacy_note)}
          </p>
          <ul className="mt-2 flex flex-col gap-2">
            {legacy.map((round) => (
              <RoundRow key={round.doc_id} round={round} />
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function RoundRow({ round }: { round: IssueHistoryRound }) {
  return (
    <li className="flex flex-col gap-0.5 text-caption">
      <div className="flex flex-wrap items-baseline gap-x-2">
        <DocLink docId={round.doc_id} label={round.id} />
        {round.station !== "" && (
          <span className="text-muted-foreground">{round.station}</span>
        )}
        {round.verdict !== "" && (
          <span className="rounded bg-muted px-1.5 py-0.5 font-mono">
            {round.verdict}
          </span>
        )}
        {round.closed_at !== "" && (
          <span className="ml-auto whitespace-nowrap tabular-nums text-muted-foreground">
            {formatDecidedAt(round.closed_at)}
          </span>
        )}
      </div>
      <p className="text-muted-foreground">{round.summary || round.title}</p>
    </li>
  );
}

function DocumentList({ documents }: { documents: IssueHistoryDocument[] }) {
  const { t } = useT("issues");
  const snapshots = documents.filter((doc) => doc.snapshot === true);
  const liveDocs = documents.filter((doc) => doc.snapshot !== true);

  return (
    <div className="flex flex-col gap-4">
      {snapshots.length > 0 && (
        <ul className="flex flex-col gap-1.5">
          {snapshots.map((doc) => (
            <li key={doc.id} className="flex flex-wrap items-baseline gap-x-2 text-caption">
              <DocLink docId={doc.id} label={doc.snapshot_of || doc.kind} />
              {doc.taken_at !== "" && (
                <span className="rounded bg-muted px-1.5 py-0.5 font-mono">
                  {doc.taken_at}
                </span>
              )}
              <span className="ml-auto whitespace-nowrap tabular-nums text-muted-foreground">
                {formatDecidedAt(doc.created_at)}
              </span>
            </li>
          ))}
        </ul>
      )}

      {/* The live documents are not snapshots and are not labelled as any: what
        they say moves under the reader, which is the opposite of what the rows
        above promise. */}
      {liveDocs.length > 0 && (
        <div>
          <p className="text-caption font-medium">
            {t(($) => $.history.documents_live_title)}
          </p>
          <ul className="mt-1.5 flex flex-col gap-1.5">
            {liveDocs.map((doc) => (
              <li key={doc.id} className="flex flex-wrap items-baseline gap-x-2 text-caption">
                <DocLink docId={doc.id} label={doc.kind} />
                <span className="text-muted-foreground">{doc.title}</span>
                <span className="ml-auto whitespace-nowrap tabular-nums text-muted-foreground">
                  {formatDecidedAt(doc.updated_at)}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

/** A row's link into the document behind it. Rows with no document — gaps —
 *  render their label as plain text, because there is nothing to open. */
function DocLink({ docId, label }: { docId: string; label: string }) {
  const { push } = useNavigation();
  const paths = useWorkspacePaths();
  if (docId === "") return <span className="font-mono">{label}</span>;
  return (
    <button
      type="button"
      onClick={() => push(paths.docDetail(docId))}
      className="font-mono font-medium underline-offset-2 hover:underline"
    >
      {label}
    </button>
  );
}
