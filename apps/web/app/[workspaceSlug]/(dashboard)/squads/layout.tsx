import type { Metadata } from "next";

/**
 * Server-side page name, so the browser tab is already correct before the
 * dashboard hydrates. Detail routes overwrite this with the entity's own name
 * once it loads — see `useDocumentTitle` in the shared view components.
 *
 * This lives in a layout rather than the page: the page modules pull in client
 * view code whose module scope calls `createContext`, which cannot be
 * evaluated in the server graph a `metadata` export would put them in.
 */
export const metadata: Metadata = { title: "Squads" };

export default function Layout({ children }: { children: React.ReactNode }) {
  return children;
}
