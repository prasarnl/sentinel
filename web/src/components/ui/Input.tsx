import { type InputHTMLAttributes, forwardRef } from "react";
import { cn } from "../../lib/cn";

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(
  ({ className, ...props }, ref) => (
    <input
      ref={ref}
      className={cn(
        "w-full rounded-md border border-[var(--border)] bg-[var(--surface-1)] px-3 py-2 text-sm text-[var(--text-primary)] outline-none",
        "focus:border-[var(--series-1)] focus:ring-1 focus:ring-[var(--series-1)]",
        "placeholder:text-[var(--text-muted)]",
        className,
      )}
      {...props}
    />
  ),
);
Input.displayName = "Input";
