import type { Issue, IssueStatus, IssuePriority } from "@multica/core/types";
import type { ActorFilterValue } from "@multica/core/issues/stores/view-store";

export interface IssueFilters {
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  creatorFilters: ActorFilterValue[];
  projectFilters: string[];
  includeNoProject: boolean;
  /** When set, result = the parent itself + its direct sub-issues. Null = no parent scoping. */
  selectedParentIssueId: string | null;
  /** When non-empty, only top-level parent issues matching one of these
   *  statuses pass, plus ALL their sub-issues (regardless of sub-issue
   *  status). Board columns are NOT hidden. */
  parentStatusFilters: IssueStatus[];
  labelFilters: string[];
}

/**
 * Filter issues using positive selection model.
 * Empty arrays = no filter (show all). Non-empty = show only matching.
 *
 * Assignee has a special "No assignee" toggle (includeNoAssignee):
 * - When only includeNoAssignee is true → show only unassigned issues
 * - When assigneeFilters has items → show only those assignees' issues
 * - When both → show matching assignees + unassigned
 */
export function filterIssues(issues: Issue[], filters: IssueFilters): Issue[] {
  const { statusFilters, priorityFilters, assigneeFilters, includeNoAssignee, creatorFilters, projectFilters, includeNoProject, selectedParentIssueId, parentStatusFilters, labelFilters } = filters;
  const hasAssigneeFilter = assigneeFilters.length > 0 || includeNoAssignee;
  const hasProjectFilter = projectFilters.length > 0 || includeNoProject;

  // Pre-compute the set of parent IDs whose status matches parentStatusFilters.
  // When the filter is active, a top-level issue passes if its own status
  // matches; a sub-issue passes if its parent's status matches (so the
  // entire parent + children tree stays visible together).
  const parentStatusMatchedIds = parentStatusFilters.length > 0
    ? new Set(
        issues
          .filter((i) => i.parent_issue_id === null && parentStatusFilters.includes(i.status))
          .map((i) => i.id),
      )
    : null;

  return issues.filter((issue) => {
    // Parent-issue scoping: keep the parent itself + its direct sub-issues.
    if (selectedParentIssueId) {
      if (issue.id !== selectedParentIssueId && issue.parent_issue_id !== selectedParentIssueId) {
        return false;
      }
    }

    // Parent status scoping: when active, a top-level issue passes iff its
    // status is selected; a sub-issue passes iff its parent's status is
    // selected (so children of a matching parent stay visible regardless of
    // the child's own status).
    if (parentStatusMatchedIds) {
      if (issue.parent_issue_id === null) {
        if (!parentStatusMatchedIds.has(issue.id)) return false;
      } else {
        if (!parentStatusMatchedIds.has(issue.parent_issue_id)) return false;
      }
    }

    if (statusFilters.length > 0 && !statusFilters.includes(issue.status))
      return false;

    if (priorityFilters.length > 0 && !priorityFilters.includes(issue.priority))
      return false;

    if (hasAssigneeFilter) {
      if (!issue.assignee_id) {
        // Unassigned issue — show only if "No assignee" is checked
        if (!includeNoAssignee) return false;
      } else if (assigneeFilters.length > 0) {
        // Assigned issue — show only if assignee is in the filter list
        if (!assigneeFilters.some(
          (f) => f.type === issue.assignee_type && f.id === issue.assignee_id,
        )) return false;
      } else {
        // Only "No assignee" is checked, no specific assignees → hide assigned issues
        return false;
      }
    }

    if (
      creatorFilters.length > 0 &&
      !creatorFilters.some(
        (f) => f.type === issue.creator_type && f.id === issue.creator_id,
      )
    ) {
      return false;
    }

    if (hasProjectFilter) {
      if (!issue.project_id) {
        if (!includeNoProject) return false;
      } else if (projectFilters.length > 0) {
        if (!projectFilters.includes(issue.project_id)) return false;
      } else {
        // Only "No project" is checked → hide issues that have a project
        return false;
      }
    }

    if (labelFilters.length > 0) {
      // OR semantics within the filter: keep issues that carry any of the
      // selected labels. Matches existing priority / project multi-select.
      const issueLabels = issue.labels;
      if (!issueLabels || issueLabels.length === 0) return false;
      if (!issueLabels.some((l) => labelFilters.includes(l.id))) return false;
    }

    return true;
  });
}
