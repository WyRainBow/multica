"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { FileText } from "lucide-react";
import type { Card } from "@multica/core/types";
import { api } from "@multica/core/api";
import {
  Command,
  CommandDialog,
  CommandInput,
  CommandList,
  CommandEmpty,
  CommandGroup,
  CommandItem,
} from "@multica/ui/components/ui/command";
import { useT } from "../../i18n";

/**
 * Pick a document that already exists, to attach to something.
 *
 * Built the way IssuePickerModal is, and for the same reason: the search runs
 * on the server, so it reaches documents this page never loaded. Filtering a
 * loaded page client-side would quietly miss anything past it — the exact
 * failure the cards handler calls out where it does the same filtering.
 *
 * Opens showing the most recent documents rather than an empty prompt. Unlike
 * issues, which number in the thousands and are found by key, a workspace has
 * few enough documents that the one you want is often already on screen.
 */
export function DocPickerModal({
  open,
  onOpenChange,
  title,
  description,
  excludeIds,
  onSelect,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  /** Documents already attached here — offering them again would be a no-op. */
  excludeIds: string[];
  onSelect: (doc: Card) => void;
}) {
  const { t } = useT("docs");
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<Card[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined);

  const run = useCallback(
    async (q: string) => {
      setIsLoading(true);
      try {
        const res = await api.listCards({
          limit: 20,
          search: q.trim() || undefined,
        });
        setResults(res.cards.filter((c) => !excludeIds.includes(c.id)));
      } catch {
        setResults([]);
      } finally {
        setIsLoading(false);
      }
    },
    [excludeIds],
  );

  // Load the recent ones on open so the dialog is useful before typing.
  useEffect(() => {
    if (!open) {
      setQuery("");
      setResults([]);
      setIsLoading(false);
      return;
    }
    void run("");
  }, [open, run]);

  const search = useCallback(
    (q: string) => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
      debounceRef.current = setTimeout(() => void run(q), 300);
    },
    [run],
  );

  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      title={title}
      description={description}
    >
      <Command shouldFilter={false}>
        <CommandInput
          placeholder={t(($) => $.picker.search_placeholder)}
          value={query}
          onValueChange={(v) => {
            setQuery(v);
            search(v);
          }}
        />
        <CommandList>
          {isLoading && (
            <div className="py-6 text-center text-body text-muted-foreground">
              {t(($) => $.picker.searching)}
            </div>
          )}
          {!isLoading && results.length === 0 && (
            <CommandEmpty>{t(($) => $.picker.no_results)}</CommandEmpty>
          )}
          {results.length > 0 && (
            <CommandGroup>
              {results.map((doc) => (
                <CommandItem
                  key={doc.id}
                  value={doc.id}
                  onSelect={() => {
                    onSelect(doc);
                    onOpenChange(false);
                  }}
                >
                  <FileText className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className="truncate">
                    {doc.title.trim() || t(($) => $.doc.untitled)}
                  </span>
                  {doc.kind && (
                    <span className="ml-auto shrink-0 text-caption text-muted-foreground">
                      {doc.kind}
                    </span>
                  )}
                  {/* A document holds ONE issue, so attaching one that is
                      already attached elsewhere MOVES it. Saying so here is
                      the only chance to notice before it happens. */}
                  {doc.issue_id && (
                    <span className="shrink-0 text-caption text-warning">
                      {t(($) => $.picker.already_linked)}
                    </span>
                  )}
                </CommandItem>
              ))}
            </CommandGroup>
          )}
        </CommandList>
      </Command>
    </CommandDialog>
  );
}
