import React from "react";
import { cn } from "@/lib/utils";

export interface SectionCardProps {
  title?: React.ReactNode;
  headerAction?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
  bodyClassName?: string;
  noPadding?: boolean;
}

export const SectionCard: React.FC<SectionCardProps> = React.memo(function SectionCard({
  title,
  headerAction,
  children,
  className,
  bodyClassName,
  noPadding = false,
}) {
  return (
    <div
      className={cn(
        "rounded-xl border border-[#1c1c1c] bg-[#111111] shadow-sm",
        className
      )}
    >
      {(title || headerAction) && (
        <div className="flex items-center justify-between border-b border-[#1c1c1c] px-5 py-4">
          {title && (
            <h2 className="text-sm font-semibold uppercase tracking-wider text-[#aaaaaa]">
              {title}
            </h2>
          )}
          {headerAction && <div>{headerAction}</div>}
        </div>
      )}

      <div className={cn(!noPadding && "p-5", bodyClassName)}>
        {children}
      </div>
    </div>
  );
});
