import ReactMarkdown from "react-markdown";

/**
 * Renderer de markdown para conteúdo estático de tutoriais/help drawers
 * (ver PolicyHelpContent). Estiliza elementos básicos com as variáveis de
 * tema do dashboard — sem depender de plugin de typography do Tailwind.
 */
export function MarkdownContent({ content }: { content: string }) {
  return (
    <div className="flex flex-col gap-3 text-[13px] leading-relaxed text-[var(--text-secondary)]">
      <ReactMarkdown
        components={{
          h2: ({ children }) => (
            <h2 className="mt-1 text-[14px] font-bold text-[var(--text-primary)]">{children}</h2>
          ),
          h3: ({ children }) => (
            <h3 className="text-[13px] font-semibold text-[var(--text-primary)]">{children}</h3>
          ),
          p: ({ children }) => <p>{children}</p>,
          strong: ({ children }) => (
            <strong className="font-semibold text-[var(--text-primary)]">{children}</strong>
          ),
          em: ({ children }) => <em className="text-[var(--text-primary)]">{children}</em>,
          code: ({ children }) => (
            <code className="rounded-[4px] bg-[var(--sunken)] px-1 py-0.5 font-mono text-[12px] text-[var(--primary)]">
              {children}
            </code>
          ),
          ul: ({ children }) => <ul className="flex flex-col gap-1.5 pl-4 list-disc">{children}</ul>,
          ol: ({ children }) => <ol className="flex flex-col gap-1.5 pl-4 list-decimal">{children}</ol>,
          li: ({ children }) => <li>{children}</li>,
          hr: () => <hr className="border-[var(--border)]" />,
          a: ({ children, href }) => (
            <a href={href} target="_blank" rel="noreferrer" className="text-[var(--primary)] underline">
              {children}
            </a>
          ),
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
}
