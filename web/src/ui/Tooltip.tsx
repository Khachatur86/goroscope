import type { ReactNode } from "react";

type Placement = "top" | "bottom" | "left" | "right";

interface TooltipProps {
  content: string;
  placement?: Placement;
  children: ReactNode;
  className?: string;
}

/**
 * Thin wrapper that attaches a CSS tooltip to its child via data attributes.
 * No JS positioning — uses pure CSS :hover. For rich/interactive content use
 * a portal-based approach instead.
 */
export function Tooltip({
  content,
  placement = "top",
  children,
  className = "",
}: TooltipProps) {
  return (
    <span
      className={`tooltip-host tooltip-host--${placement} ${className}`}
      data-tooltip={content}
    >
      {children}
    </span>
  );
}
