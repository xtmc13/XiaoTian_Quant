import React from "react";
import { cn } from "@/lib/utils";

export interface SkeletonProps {
  variant?: "text" | "card" | "circle" | "rect";
  width?: string | number;
  height?: string | number;
  className?: string;
  lines?: number;
}

export const Skeleton: React.FC<SkeletonProps> = ({
  variant = "text",
  width,
  height,
  className,
  lines = 1,
}) => {
  const baseClasses =
    "bg-[#1c1c1c] animate-pulse rounded-md";

  const style: React.CSSProperties = {
    width: width ?? undefined,
    height: height ?? undefined,
  };

  if (variant === "text") {
    return (
      <div className="flex flex-col gap-2">
        {Array.from({ length: lines }).map((_, i) => (
          <div
            key={i}
            className={cn(
              baseClasses,
              "h-4 w-full rounded",
              className
            )}
            style={{
              ...style,
              width:
                width ??
                (i === lines - 1 && lines > 1 ? "75%" : "100%"),
            }}
          />
        ))}
      </div>
    );
  }

  if (variant === "circle") {
    return (
      <div
        className={cn(baseClasses, "rounded-full", className)}
        style={{
          ...style,
          width: width ?? "40px",
          height: height ?? "40px",
        }}
      />
    );
  }

  if (variant === "card") {
    return (
      <div
        className={cn(
          baseClasses,
          "rounded-xl border border-[#1c1c1c]",
          className
        )}
        style={{
          ...style,
          width: width ?? "100%",
          height: height ?? "120px",
        }}
      />
    );
  }

  // rect
  return (
    <div
      className={cn(baseClasses, "rounded-lg", className)}
      style={{
        ...style,
        width: width ?? "100%",
        height: height ?? "80px",
      }}
    />
  );
};
