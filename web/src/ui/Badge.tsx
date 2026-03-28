import type { ReactNode } from "react";

type Variant = "state" | "legend" | "diff" | "insight";

interface BadgeProps {
  variant?: Variant;
  /** For state/legend badges: the goroutine state (RUNNING, WAITING, …) */
  state?: string;
  className?: string;
  children: ReactNode;
}

export function Badge({
  variant = "state",
  state,
  className = "",
  children,
}: BadgeProps) {
  const cls = [
    "badge",
    `badge--${variant}`,
    state ?? "",
    className,
  ]
    .filter(Boolean)
    .join(" ");

  return <span className={cls}>{children}</span>;
}
