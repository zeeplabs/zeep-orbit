import { cn } from "@/lib/utils";

interface ShineBorderProps {
  borderWidth?: number;
  duration?: number;
  color?: string | string[];
  className?: string;
  children: React.ReactNode;
  style?: React.CSSProperties;
}

export function ShineBorder({
  borderWidth = 1,
  duration = 14,
  color = ["#0347A5", "#7C3AED", "#EC4899"],
  className,
  style,
  children,
}: ShineBorderProps) {
  return (
    <div
      style={
        {
          "--border-width": `${borderWidth}px`,
          "--duration": `${duration}s`,
          backgroundImage: `radial-gradient(ellipse, ${Array.isArray(color) ? color.join(",") : color}, transparent 10%)`,
          backgroundSize: "300% 300%",
          "--shine-degree": "120deg",
          ...style,
        } as React.CSSProperties
      }
      className={cn(
        "relative grid min-h-[60px] w-fit place-items-center rounded-[inherit] p-[--border-width] will-change-[background-position] before:absolute before:inset-0 before:block before:rounded-[inherit] before:p-[--border-width] before:content-['']",
        "animate-[shine_var(--duration)_infinite_linear]",
        className,
      )}
    >
      {children}
    </div>
  );
}
