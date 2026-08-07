export function AboutPanel({ title, lines }: { title: string; lines: string[] }) {
  return (
    <div className="sticky top-0 flex w-full max-w-[380px] flex-1 flex-col gap-3 rounded-[14px] border border-[var(--border)] bg-[var(--surface)] p-5">
      <div className="text-[13px] font-bold text-[var(--text-primary)]">{title}</div>
      {lines.map((line, i) => (
        <p key={i} className="text-[12.5px] leading-relaxed text-[var(--text-secondary)]">
          {line}
        </p>
      ))}
    </div>
  );
}
