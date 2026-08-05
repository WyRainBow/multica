import { useT } from "./use-t";

// Absolute-time formatter, the counterpart to useTimeAgo.
//
// "24 分钟前" answers "how long ago" and refuses to answer "when". On a
// requirement that ran for weeks that is the wrong question: the reader is
// reconstructing an order of events, and two entries both reading "2 天前"
// carry no order at all. So the issue timeline prints the clock instead.
//
// 24-hour and locale-formatted, seconds included — the timeline is where an
// agent's write and a person's reply can land in the same minute.
export function useExactTime() {
  const { i18n } = useT("common");
  return (dateStr: string | null | undefined, withSeconds = true): string => {
    if (!dateStr) return "—";
    const parsed = new Date(dateStr);
    if (Number.isNaN(parsed.getTime())) return "—";
    return new Intl.DateTimeFormat(i18n.language, {
      year: "numeric",
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      ...(withSeconds ? { second: "2-digit" as const } : {}),
      hour12: false,
    }).format(parsed);
  };
}
