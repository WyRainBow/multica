-- Inline comments anchor to a span of the issue description. The description
-- is stored as Markdown, which has no representation for a highlight, so the
-- anchor lives on the comment instead of as a mark in the document: we keep
-- the quoted text and a character-offset hint, then re-locate the span at
-- render time. A comment whose text no longer appears simply stops
-- highlighting and reads as an ordinary comment — it is never lost.
ALTER TABLE comment ADD COLUMN anchor_text TEXT;
ALTER TABLE comment ADD COLUMN anchor_offset INTEGER;
