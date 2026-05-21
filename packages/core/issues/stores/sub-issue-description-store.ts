import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

/**
 * Global preference for whether the description section is collapsed on
 * sub-issue (issues with a parent_issue_id) detail pages.
 *
 * - Default: true (collapsed). Sub-issues are usually opened to see comments
 *   / activity, not the long template-generated description.
 * - User toggle persists across sessions and across all sub-issues — this is
 *   intentionally a single global flag, NOT a per-issue map. Per-issue
 *   memory would be confusing ("why is THIS one expanded?") and
 *   localStorage-noisy on workspaces with hundreds of sub-issues.
 *
 * Top-level issues (no parent) ignore this store entirely — their description
 * is always expanded since it IS their primary content.
 */
interface SubIssueDescriptionStore {
  collapsed: boolean;
  setCollapsed: (collapsed: boolean) => void;
  toggle: () => void;
}

export const useSubIssueDescriptionStore = create<SubIssueDescriptionStore>()(
  persist(
    (set, get) => ({
      collapsed: true,
      setCollapsed: (collapsed) => set({ collapsed }),
      toggle: () => set({ collapsed: !get().collapsed }),
    }),
    {
      name: "multica_sub_issue_description_collapsed",
      storage: createJSONStorage(() => defaultStorage),
    },
  ),
);
