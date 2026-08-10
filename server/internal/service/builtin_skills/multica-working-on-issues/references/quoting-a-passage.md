# Reading one passage of a description

`multica issue get <id>` returns the whole description. On a long requirement
that is most of a context budget spent to read one section — and worse, when
you locate the section yourself you can locate it wrongly, and nothing
downstream can tell that the review you wrote was of the wrong text.

The quote flags return one span instead.

## The flags

```bash
multica issue get COC-45 \
  --quote-start "路由决策链路" \
  --quote-end   "暂时如此。" \
  [--quote-prefix "..."] [--quote-suffix "..."]
```

| Flag | What it is |
| --- | --- |
| `--quote-start` | Text the passage begins with, copied verbatim. Required. |
| `--quote-end` | Text it ends with. Omit to return the start text alone. |
| `--quote-prefix` | Text immediately BEFORE the passage. Only needed to disambiguate. |
| `--quote-suffix` | Text immediately AFTER it. Same purpose. |

**Both edges must land in exactly one place.** A bare `--quote-end "。"` is
rejected, not resolved to the first sentence end — copy enough of the
passage's last line to be unique, or pin what follows it with
`--quote-suffix`. The two edges fail differently and the error says which:
a repeated START needs surrounding context, a repeated END needs a longer end,
and no amount of `--quote-prefix` changes where a span stops.

The response is the span, plus `identifier`, `title` and the character offsets
it was cut at. The rest of the description is not returned.

## Why edges and not the passage

Quoting a passage in order to locate it defeats the point: a 1000-character
selection would need a 1000-character handle. Edges stay short however long the
span is, which is what makes a handle small enough to paste into a message.

This is W3C's `TextQuoteSelector` (exact/prefix/suffix) widened to a span. The
URL form of the same idea — Text Fragments, `#:~:text=start,end` — is
deliberately not used: packing four strings into one slot forces
percent-encoding of `,` and `-`, and a half-width comma is ordinary in Chinese
prose. Flags need no escaping.

## It refuses rather than guesses

Zero matches and several matches are both errors, on either edge:

```
the quote matches 2 spans in the description; add --quote-prefix with the text
just before the passage (or --quote-suffix with the text just after) to say
which one

--quote-end matches 27 places after the start, so the passage has no single
ending; copy more of the passage's last line into --quote-end, or add
--quote-suffix with the text just after it
```

Returning the first of several candidates is the one outcome worth engineering
against — the caller is usually an agent about to review whatever comes back,
and a confident review of the wrong passage looks exactly like a correct one.
A truncated span is that same failure: an end that stops at the first sentence
of a ten-paragraph section returns something that reads complete.

An edge flag without `--quote-start` is also an error, not an ignored flag:
ignoring it would print the whole description, which reads like a quote that
worked and happened to be long.

## Whitespace is matched loosely

A run of whitespace in an edge matches any run in the description, so an edge
that travelled through a one-line command — where a paragraph break arrives as
a single space — still matches its source. Everything else is exact: dropping a
character inside an edge is a miss.

## Where a handle comes from

In the app, select the passage in an issue description and use the clipboard
button in the selection toolbar. It writes the whole command line, with the
edges and any tie-breaker already chosen and already checked against the stored
Markdown. Nobody has to type these flags.

The button is absent on a finished issue (`done` / `cancelled`), whose
description renders read-only with no selection toolbar. The flags still work
there; they just have to be written by hand.

## Writing back to a passage: anchored comments

The mirror of reading a span is commenting on one. An ordinary comment is about
the issue; an INLINE comment is about specific words of its description — use it
when what you are saying only makes sense next to them: explaining a paragraph,
questioning a sentence, flagging a term.

```bash
multica issue comment add <id> --anchor "V1 结论" --content "..."
multica issue comment add <id> --anchor "V1 结论" --anchor-occurrence 2 --content "..."
```

`--anchor` takes the passage VERBATIM out of the current description. The CLI
locates it and computes the offset; never supply one. Two rules follow:

- **Copy, do not paraphrase.** A passage that does not appear exactly is a hard
  error, not a warning. Deliberate: without it you would file a comment that
  silently highlights nothing, and the mistake would surface much later as "the
  feature is broken".
- **Disambiguate repeats with `--anchor-occurrence`.** When the passage occurs
  more than once the error says how many times; pick the one you meant rather
  than widening the passage until it happens to be unique.

The comment then behaves like any other — replies thread under it, it can be
resolved. The anchor only adds *where in the description* it is about.

If the description is later edited so the passage no longer appears, the comment
survives and stops highlighting. Nothing is lost, so prefer an anchored comment
whenever a passage is what you are talking about.

Note the two are independent mechanisms: reading a span uses a quote, writing to
one uses an anchor. A quote is consumed immediately; an anchor persists on the
comment.

## Related

- Reading one comment instead of a thread: `multica issue comment get <id>`.
- How a thread is shaped and when to resolve it: `references/comment-threads.md`.
