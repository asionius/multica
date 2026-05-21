import { useState, useCallback, type MouseEvent } from "react";
import { timeAgo } from "@multica/core/utils";
import { cn } from "@multica/ui/lib/utils";

interface RelativeTimestampProps {
  /** ISO date string. */
  date: string;
  /** Extra classes for the underlying span. */
  className?: string;
  /**
   * When true, shows absolute time first and toggles to relative on click.
   * Defaults to false (relative first).
   */
  absoluteFirst?: boolean;
}

/**
 * A timestamp that toggles between relative ("2d ago") and absolute
 * ("5/9/2026, 10:30:45 AM") representation when clicked.
 *
 * Used in issue activity history and comments where users sometimes need
 * the precise time without losing the at-a-glance "how long ago" view.
 */
export function RelativeTimestamp({
  date,
  className,
  absoluteFirst = false,
}: RelativeTimestampProps) {
  const [showAbsolute, setShowAbsolute] = useState(absoluteFirst);

  const handleClick = useCallback((e: MouseEvent<HTMLSpanElement>) => {
    // Avoid bubbling to row/card click handlers.
    e.stopPropagation();
    setShowAbsolute((v) => !v);
  }, []);

  const relative = timeAgo(date);
  const absolute = new Date(date).toLocaleString();

  return (
    <span
      role="button"
      tabIndex={0}
      onClick={handleClick}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          setShowAbsolute((v) => !v);
        }
      }}
      title={showAbsolute ? relative : absolute}
      className={cn("cursor-pointer select-none", className)}
    >
      {showAbsolute ? absolute : relative}
    </span>
  );
}
