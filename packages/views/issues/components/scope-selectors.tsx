"use client";

import { useMemo, useState } from "react";
import { ChevronDown, FolderKanban, FolderMinus, GitBranch, Check } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuCheckboxItem,
  DropdownMenuItem,
} from "@multica/ui/components/ui/dropdown-menu";
import { useWorkspaceId } from "@multica/core/hooks";
import { projectListOptions } from "@multica/core/projects/queries";
import { useViewStore, useViewStoreApi } from "@multica/core/issues/stores/view-store-context";
import { useT } from "../../i18n";
import type { Issue } from "@multica/core/types";

const ITEM_CLASS =
  "group/item pr-1.5! [&>[data-slot=dropdown-menu-checkbox-item-indicator]]:hidden";

function HoverCheck({ checked }: { checked: boolean }) {
  return (
    <div
      className="border-input data-[selected=true]:border-primary data-[selected=true]:bg-primary data-[selected=true]:text-primary-foreground pointer-events-none size-4 shrink-0 rounded-[4px] border transition-all select-none *:[svg]:opacity-0 data-[selected=true]:*:[svg]:opacity-100 opacity-0 group-hover/item:opacity-100 group-focus/item:opacity-100 data-[selected=true]:opacity-100"
      data-selected={checked}
    >
      <Check className="size-3.5 text-current" />
    </div>
  );
}

/**
 * ScopeSelectors — promoted Project + Parent Issue selectors.
 *
 * Sits above the main filter row. Users expect these two as the primary way
 * to narrow the issues view (version → requirement → flow), so they get
 * first-class UI instead of being buried inside the generic Filter menu.
 *
 * - Project: multi-select (mirrors the behavior already in the filter menu;
 *   both entry points write to the same store fields).
 * - Parent Issue: single-select. When set, filterIssues() keeps only the
 *   parent itself and its direct sub-issues.
 */
export function ScopeSelectors({ allIssues }: { allIssues: Issue[] }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();

  const projectFilters = useViewStore((s) => s.projectFilters);
  const includeNoProject = useViewStore((s) => s.includeNoProject);
  const selectedParentIssueId = useViewStore((s) => s.selectedParentIssueId);
  const storeApi = useViewStoreApi();

  const { data: projects = [] } = useQuery(projectListOptions(wsId));

  // Parent candidates: top-level issues (parent_issue_id === null), optionally
  // scoped to the selected projects.
  const parentCandidates = useMemo(() => {
    const hasProjectScope = projectFilters.length > 0 || includeNoProject;
    const tops = allIssues.filter((i) => i.parent_issue_id === null);
    if (!hasProjectScope) return tops;
    return tops.filter((i) => {
      if (!i.project_id) return includeNoProject;
      return projectFilters.includes(i.project_id);
    });
  }, [allIssues, projectFilters, includeNoProject]);

  const selectedParent = useMemo(
    () => allIssues.find((i) => i.id === selectedParentIssueId) ?? null,
    [allIssues, selectedParentIssueId],
  );

  const projectLabel = useMemo(() => {
    const total = projectFilters.length + (includeNoProject ? 1 : 0);
    if (total === 0) return t(($) => $.scopeBar.allProjects);
    if (total === 1) {
      if (includeNoProject && projectFilters.length === 0) {
        return t(($) => $.scopeBar.noProject);
      }
      const only = projects.find((p) => p.id === projectFilters[0]);
      return only?.title ?? t(($) => $.scopeBar.project);
    }
    return `${total} ${t(($) => $.scopeBar.project).toLowerCase()}`;
  }, [projectFilters, includeNoProject, projects, t]);

  const parentLabel = selectedParent
    ? selectedParent.title
    : t(($) => $.scopeBar.allIssues);

  return (
    <div className="flex items-center gap-2 border-b px-4 py-2">
      {/* Project selector (multi-select, promoted from Filter menu) */}
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button variant="outline" size="sm" className="h-7 gap-1.5">
              <FolderKanban className="size-3.5" />
              <span className="text-xs font-medium">
                {t(($) => $.scopeBar.project)}:
              </span>
              <span className="text-xs truncate max-w-[160px]">{projectLabel}</span>
              <ChevronDown className="size-3.5 opacity-50" />
            </Button>
          }
        />
        <DropdownMenuContent align="start" className="w-64 p-0">
          <ProjectSelectorContent />
        </DropdownMenuContent>
      </DropdownMenu>

      {/* Parent Issue selector (single-select) */}
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button variant="outline" size="sm" className="h-7 gap-1.5">
              <GitBranch className="size-3.5" />
              <span className="text-xs font-medium">
                {t(($) => $.scopeBar.parentIssue)}:
              </span>
              <span className="text-xs truncate max-w-[220px]">{parentLabel}</span>
              <ChevronDown className="size-3.5 opacity-50" />
            </Button>
          }
        />
        <DropdownMenuContent align="start" className="w-80 p-0">
          <ParentSelectorContent
            candidates={parentCandidates}
            selected={selectedParentIssueId}
            onSelect={(id) => storeApi.getState().setSelectedParentIssueId(id)}
          />
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Project selector content — multi-select with "No project" toggle.
// Mirrors ProjectSubContent in issues-header.tsx but writes via useViewStore.
// ---------------------------------------------------------------------------

function ProjectSelectorContent() {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const storeApi = useViewStoreApi();
  const projectFilters = useViewStore((s) => s.projectFilters);
  const includeNoProject = useViewStore((s) => s.includeNoProject);

  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const [search, setSearch] = useState("");
  const query = search.trim().toLowerCase();
  const filtered = projects.filter((p) => p.title.toLowerCase().includes(query));

  return (
    <>
      <div className="px-2 py-1.5 border-b border-foreground/5">
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t(($) => $.scopeBar.searchProject)}
          className="w-full bg-transparent text-sm placeholder:text-muted-foreground outline-none"
          autoFocus
        />
      </div>

      <div className="max-h-64 overflow-y-auto p-1">
        {(!query || "no project".includes(query)) && (
          <DropdownMenuCheckboxItem
            checked={includeNoProject}
            onCheckedChange={() => storeApi.getState().toggleNoProject()}
            className={ITEM_CLASS}
          >
            <HoverCheck checked={includeNoProject} />
            <FolderMinus className="size-3.5 text-muted-foreground" />
            {t(($) => $.scopeBar.noProject)}
          </DropdownMenuCheckboxItem>
        )}

        {filtered.map((p) => {
          const checked = projectFilters.includes(p.id);
          return (
            <DropdownMenuCheckboxItem
              key={p.id}
              checked={checked}
              onCheckedChange={() => storeApi.getState().toggleProjectFilter(p.id)}
              className={ITEM_CLASS}
            >
              <HoverCheck checked={checked} />
              <span className="size-3.5 flex items-center justify-center shrink-0">
                {p.icon || <FolderKanban className="size-3.5 text-muted-foreground" />}
              </span>
              <span className="truncate">{p.title}</span>
            </DropdownMenuCheckboxItem>
          );
        })}

        {filtered.length === 0 && search && (
          <div className="px-2 py-3 text-center text-sm text-muted-foreground">
            {t(($) => $.filters.no_results)}
          </div>
        )}
      </div>
    </>
  );
}

// ---------------------------------------------------------------------------
// Parent Issue selector content — single-select with "All issues" reset.
// ---------------------------------------------------------------------------

function ParentSelectorContent({
  candidates,
  selected,
  onSelect,
}: {
  candidates: Issue[];
  selected: string | null;
  onSelect: (id: string | null) => void;
}) {
  const { t } = useT("issues");
  const [search, setSearch] = useState("");
  const query = search.trim().toLowerCase();
  const filtered = useMemo(() => {
    if (!query) return candidates;
    return candidates.filter(
      (i) =>
        i.title.toLowerCase().includes(query) ||
        i.identifier.toLowerCase().includes(query),
    );
  }, [candidates, query]);

  return (
    <>
      <div className="px-2 py-1.5 border-b border-foreground/5">
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t(($) => $.scopeBar.searchIssue)}
          className="w-full bg-transparent text-sm placeholder:text-muted-foreground outline-none"
          autoFocus
        />
      </div>

      <div className="max-h-80 overflow-y-auto p-1">
        <DropdownMenuItem
          onClick={() => onSelect(null)}
          className="group/item"
        >
          <div
            className="border-input data-[selected=true]:border-primary data-[selected=true]:bg-primary data-[selected=true]:text-primary-foreground pointer-events-none size-4 shrink-0 rounded-full border transition-all select-none"
            data-selected={selected === null}
          />
          <span>{t(($) => $.scopeBar.allIssues)}</span>
        </DropdownMenuItem>

        {filtered.length === 0 && (
          <div className="px-2 py-3 text-center text-sm text-muted-foreground">
            {query ? t(($) => $.filters.no_results) : t(($) => $.scopeBar.noParentIssues)}
          </div>
        )}

        {filtered.map((issue) => {
          const isSelected = selected === issue.id;
          return (
            <DropdownMenuItem
              key={issue.id}
              onClick={() => onSelect(issue.id)}
              className="group/item"
            >
              <div
                className="border-input data-[selected=true]:border-primary data-[selected=true]:bg-primary data-[selected=true]:text-primary-foreground pointer-events-none size-4 shrink-0 rounded-full border transition-all select-none"
                data-selected={isSelected}
              />
              <span className="text-xs text-muted-foreground shrink-0 font-mono">
                {issue.identifier}
              </span>
              <span className="truncate" title={issue.title}>{issue.title}</span>
            </DropdownMenuItem>
          );
        })}
      </div>
    </>
  );
}
