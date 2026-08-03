import type { Metadata } from "next";

/**
 * Server-side page name, so the browser tab is already correct before the
 * dashboard hydrates.
 *
 * This lives in a layout rather than the page: the page module pulls in client
 * view code whose module scope calls `createContext`, which cannot be
 * evaluated in the server graph a `metadata` export would put it in.
 */
export const metadata: Metadata = { title: "Retros" };

export default function Layout({ children }: { children: React.ReactNode }) {
  return children;
}
