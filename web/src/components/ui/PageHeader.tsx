import React from "react";
import { cn } from "@/lib/utils";

export interface PageHeaderProps {
  title?: string;
  subtitle?: string;
  actions?: React.ReactNode;
  className?: string;
}

export const PageHeader: React.FC<PageHeaderProps> = ({
  title,
  subtitle,
  actions,
  className,
}) => {
  return (
    <div
      className={cn(
        "flex flex-col gap-1 pb-3 sm:flex-row sm:items-end sm:justify-between",
        className
      )}
    >
      <div>
        {title && (
          <h1 className="text-xl font-semibold tracking-tight text-white sm:text-2xl">
            {title}
          </h1>
        )}
        {subtitle && (
          <p className="mt-1 text-sm text-[#666666]">{subtitle}</p>
        )}
      </div>

      {actions && (
        <div className="mt-3 flex items-center gap-2 sm:mt-0">
          {actions}
        </div>
      )}
    </div>
  );
};
