import type { HTMLAttributes, ReactNode } from "react";

type Level = 1 | 2 | 3 | 4;

interface HeadingProps extends HTMLAttributes<HTMLHeadingElement> {
  level?: Level;
  children: ReactNode;
}

export function Heading({ level = 2, className = "", children, ...rest }: HeadingProps) {
  const Tag = `h${level}` as "h1" | "h2" | "h3" | "h4";
  return (
    <Tag className={`heading heading--${level} ${className}`} {...rest}>
      {children}
    </Tag>
  );
}
