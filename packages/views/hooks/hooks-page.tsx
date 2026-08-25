"use client";

import { useT } from "../i18n";
import { HooksTab } from "../settings/components/hooks-tab";

/**
 * The workspace's hook inventory as a destination of its own.
 *
 * It reads like a setting but it is not one: nothing here is configurable, and
 * what it shows is a live observation of other machines — which runtime saw
 * which hook, and whether it has fired since anything was watching. That makes
 * it a page you go and look at, next to runtimes, rather than a form you open
 * to change something.
 */
export function HooksPage() {
  const { t } = useT("settings");
  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <div className="mx-auto w-full max-w-5xl p-4 sm:p-6 md:p-8">
        <h1 className="mb-1 text-title font-medium text-foreground">{t(($) => $.hooks.title)}</h1>
        <p className="mb-6 text-body text-muted-foreground">{t(($) => $.hooks.description)}</p>
        <HooksTab />
      </div>
    </div>
  );
}
