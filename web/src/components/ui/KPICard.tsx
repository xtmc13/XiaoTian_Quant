import React from "react";
import { cn } from "@/lib/utils";
import { ArrowUpRight, ArrowDownRight, ChevronRight } from "lucide-react";

export interface KPICardProps {
  icon: React.ReactNode;
  label: string;
  value: string | number;
  subValue?: string | number;
  subLabel?: string;
  trend?: "up" | "down" | "neutral";
  ringProgress?: number; // 0 - 100
  primary?: boolean;
  onClick?: () => void;
  onNavigate?: () => void;
  className?: string;
}

export const KPICard: React.FC<KPICardProps> = ({
  icon,
  label,
  value,
  subValue,
  subLabel,
  trend = "neutral",
  ringProgress,
  primary = false,
  onClick,
  onNavigate,
  className,
}) => {
  const size = 56;
  const stroke = 4;
  const radius = (size - stroke) / 2;
  const circumference = 2 * Math.PI * radius;
  const offset =
    ringProgress !== undefined
      ? circumference - (ringProgress / 100) * circumference
      : circumference;

  return (
    <div
      onClick={onClick}
      className={cn(
        "relative rounded-xl border bg-[#111111] p-5 transition-all duration-300",
        "border-[#1c1c1c] hover:border-[#2a2a2a]",
        primary &&
          "border-[#2a2a2a] shadow-[0_0_20px_rgba(255,255,255,0.04)]",
        onClick && "cursor-pointer",
        className
      )}
    >
      {/* Glow effect for primary card */}
      {primary && (
        <div className="pointer-events-none absolute inset-0 rounded-xl ring-1 ring-inset ring-white/5" />
      )}

      <div className="flex items-start justify-between">
        {/* Left content */}
        <div className="flex-1">
          <div className="flex items-center gap-2 text-[#888888]">
            <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-[#141414]">
              {icon}
            </span>
            <span className="text-xs font-medium uppercase tracking-wider">
              {label}
            </span>
          </div>

          <div className="mt-3">
            <div className="text-2xl font-semibold tracking-tight text-white">
              {value}
            </div>

            {(subValue || subLabel) && (
              <div className="mt-1 flex items-center gap-2">
                {trend !== "neutral" && (
                  <span
                    className={cn(
                      "flex items-center text-xs font-medium",
                      trend === "up" ? "text-emerald-400" : "text-red-400"
                    )}
                  >
                    {trend === "up" ? (
                      <ArrowUpRight className="mr-0.5 h-3 w-3" />
                    ) : (
                      <ArrowDownRight className="mr-0.5 h-3 w-3" />
                    )}
                    {subValue}
                  </span>
                )}
                {trend === "neutral" && subValue && (
                  <span className="text-xs font-medium text-[#666666]">
                    {subValue}
                  </span>
                )}
                {subLabel && (
                  <span className="text-xs text-[#555555]">{subLabel}</span>
                )}
              </div>
            )}
          </div>
        </div>

        {/* Right: ring progress + navigate arrow */}
        <div className="flex flex-col items-end gap-3">
          {ringProgress !== undefined && (
            <div className="relative" style={{ width: size, height: size }}>
              <svg
                width={size}
                height={size}
                className="-rotate-90 transform"
              >
                <circle
                  cx={size / 2}
                  cy={size / 2}
                  r={radius}
                  fill="none"
                  stroke="#1c1c1c"
                  strokeWidth={stroke}
                />
                <circle
                  cx={size / 2}
                  cy={size / 2}
                  r={radius}
                  fill="none"
                  stroke="currentColor"
                  strokeWidth={stroke}
                  strokeLinecap="round"
                  strokeDasharray={circumference}
                  strokeDashoffset={offset}
                  className={cn(
                    "transition-all duration-700 ease-out",
                    primary ? "text-white" : "text-[#888888]"
                  )}
                />
              </svg>
              <div className="absolute inset-0 flex items-center justify-center">
                <span
                  className={cn(
                    "text-xs font-semibold",
                    primary ? "text-white" : "text-[#aaaaaa]"
                  )}
                >
                  {Math.round(ringProgress)}%
                </span>
              </div>
            </div>
          )}

          {onNavigate && (
            <button
              onClick={(e) => {
                e.stopPropagation();
                onNavigate();
              }}
              className="flex h-7 w-7 items-center justify-center rounded-full bg-[#141414] text-[#888888] transition-colors hover:bg-[#1c1c1c] hover:text-white"
            >
              <ChevronRight className="h-4 w-4" />
            </button>
          )}
        </div>
      </div>
    </div>
  );
};
