import type { ButtonHTMLAttributes, ReactNode } from "react";

type Variant = "primary" | "secondary" | "ghost" | "danger" | "chip" | "icon";
type Size = "sm" | "base";

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  size?: Size;
  children: ReactNode;
}

export function Button({
  variant = "primary",
  size,
  className = "",
  children,
  ...rest
}: ButtonProps) {
  const cls = [
    "btn",
    `btn--${variant}`,
    size === "sm" ? "btn--icon" : "",
    className,
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <button type="button" className={cls} {...rest}>
      {children}
    </button>
  );
}
