"use client";

import { useEffect } from "react";

/**
 * Appended to every title this hook writes. Web sets " | Multica" so
 * client-rendered titles read like the ones Next.js builds from its
 * `%s | Multica` metadata template; desktop leaves it empty because the same
 * string is also the tab label, where a repeated app name is pure noise.
 */
let titleSuffix = "";

/** Call once during app bootstrap, before any page renders. */
export function configureDocumentTitle(options: { suffix?: string }) {
  titleSuffix = options.suffix ?? "";
}

/**
 * Sets `document.title` for the page currently on screen.
 *
 * Lives in the shared view layer rather than each app's routing code because
 * both platforms want the same answer to "what is this page called" — web
 * shows it in the browser tab, desktop's tab system observes `document.title`
 * through a MutationObserver and uses it for both the tab label and the OS
 * window title.
 *
 * Pass a falsy title while the entity is still loading and the previous title
 * stays put, which reads better than flashing a placeholder between two real
 * titles when navigating detail page to detail page.
 */
export function useDocumentTitle(title: string | undefined | null) {
  useEffect(() => {
    if (!title) return;
    document.title = titleSuffix ? title + titleSuffix : title;
  }, [title]);
}
