import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

/**
 * Display preference for the issue-description outline.
 *
 * Collapsing it is a personal reading-ergonomics choice, like the sticky
 * composer next door: someone who wants the widest possible prose column
 * wants it on every issue, not just the one they happened to collapse it on.
 * Persisted globally via `defaultStorage` for the same reason.
 */
interface DescriptionOutlineStore {
  collapsed: boolean;
  toggleCollapsed: () => void;
}

export const useDescriptionOutlineStore = create<DescriptionOutlineStore>()(
  persist(
    (set) => ({
      collapsed: false,
      toggleCollapsed: () => set((s) => ({ collapsed: !s.collapsed })),
    }),
    {
      name: "multica_description_outline",
      storage: createJSONStorage(() => defaultStorage),
    },
  ),
);
