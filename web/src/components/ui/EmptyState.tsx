import React from "react";
import { cn } from "@/lib/utils";
import { PackageOpen } from "lucide-react";

export interface EmptyStateProps {
  icon?: React.ReactNode;
  title: string;
  description?: string;
  actionLabel?: string;
  onAction?: () => void;
  className?: string;
}

export const EmptyState: React.FC<EmptyStateProps> = ({
  icon,
  title,
  description,
  actionLabel,
  onAction,
  className,
}) => {
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center rounded-xl border border-[#1c1c1c] bg-[#111111] px-6 py-16 text-center",
        className
      )}
    >
      <div className="flex h-14 w-14 items-center justify-center rounded-full bg-[#141414] text-[#999999]">
        {icon ?? <PackageOpen className="h-6 w-6" />}
      </div>

      <h3 className="mt-5 text-base font-semibold text-white">{title}</h3>

      {description && (
        <p className="mt-2 max-w-xs text-sm leading-relaxed text-[#999999]">
          {description}
        </p>
      )}

      {actionLabel && onAction && (
        <button
          onClick={onAction}
          className="mt-6 rounded-lg bg-white px-4 py-2 text-sm font-medium text-[#0a0a0a] transition-opacity hover:opacity-90"
        >
          {actionLabel}
        </button>
      )}
    </div>
  );
};
