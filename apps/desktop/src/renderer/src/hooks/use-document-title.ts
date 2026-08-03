/**
 * Re-export of the shared hook so desktop-only pages keep a local import path.
 * Pages built on a shared view component do NOT call this — the component sets
 * its own title, which is what keeps web and desktop naming pages identically.
 */
export { useDocumentTitle } from "@multica/views/platform";
