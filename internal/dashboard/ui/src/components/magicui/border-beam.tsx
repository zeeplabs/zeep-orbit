import { cn } from "@/lib/utils";

interface BorderBeamProps {
  className?: string;
  size?: number;
  duration?: number;
  colorFrom?: string;
  colorTo?: string;
  borderWidth?: number;
}

export function BorderBeam({
  className,
  size = 200,
  duration = 15,
  colorFrom = "#0347A5",
  colorTo = "#7C3AED",
  borderWidth = 1.5,
}: BorderBeamProps) {
  return (
    <div
      className={cn(
        "pointer-events-none absolute inset-[-1px] rounded-[inherit] overflow-hidden",
        className,
      )}
    >
      {/* rotating conic gradient — creates the beam */}
      <div
        style={{
          background: `conic-gradient(from 0deg at 50% 50%, transparent 0deg, ${colorFrom} 60deg, ${colorTo} 120deg, transparent 180deg)`,
          animationDuration: `${duration}s`,
        }}
        className="absolute inset-[-50%] aspect-square animate-[border-beam_linear_infinite] opacity-50"
      />
      {/* inner cutout — shows only the border ring, hides the center */}
      <div
        style={{ inset: `${borderWidth}px` }}
        className="absolute rounded-[inherit] bg-[#0A0A0F]"
      />
    </div>
  );
}
