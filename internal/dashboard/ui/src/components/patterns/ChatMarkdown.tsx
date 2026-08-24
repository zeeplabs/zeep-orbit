import ReactMarkdown from "react-markdown";

/**
 * Renderer de markdown para bolhas de chat (Build/Edit with AI). Diferente
 * de MarkdownContent (tutoriais/help), não fixa cor de texto própria —
 * herda a cor da bolha que o envolve (branco na bolha do usuário, texto
 * primário na bolha do assistente) via `color: inherit` em cada elemento.
 */
export function ChatMarkdown({ content }: { content: string }) {
  return (
    <div className="flex flex-col gap-2 [&_*]:text-inherit">
      <ReactMarkdown
        components={{
          p: ({ children }) => <p>{children}</p>,
          strong: ({ children }) => <strong className="font-semibold">{children}</strong>,
          em: ({ children }) => <em>{children}</em>,
          code: ({ children }) => (
            <code className="rounded-[4px] bg-black/10 px-1 py-0.5 font-mono text-[12px]">{children}</code>
          ),
          ul: ({ children }) => <ul className="flex flex-col gap-1 pl-4 list-disc">{children}</ul>,
          ol: ({ children }) => <ol className="flex flex-col gap-1 pl-4 list-decimal">{children}</ol>,
          li: ({ children }) => <li>{children}</li>,
          a: ({ children, href }) => (
            <a href={href} target="_blank" rel="noreferrer" className="underline">
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
