import { useState } from "react";
import { motion } from "framer-motion";
import {
  Database,
  Table2,
  ChevronDown,
  ChevronRight,
  RefreshCw,
  ArrowUpDown,
  ArrowUp,
  ArrowDown,
  ChevronLeft,
  ChevronRight as ChevronRightIcon,
  Loader2,
} from "lucide-react";
import { useDataBrowserApps, useDataBrowserQuery, DataBrowserApp, DataBrowserTable } from "../lib/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";

const ease = [0.32, 0.72, 0, 1] as const;

const fadeUp = {
  initial: { opacity: 0, y: 16 },
  animate: { opacity: 1, y: 0 },
  transition: { duration: 0.6, ease },
};

function TableSkeleton({ cols }: { cols: number }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 8, padding: 16 }}>
      <div
        style={{
          display: "grid",
          gridTemplateColumns: `repeat(${Math.min(cols, 6)}, 1fr)`,
          gap: 12,
        }}
      >
        {Array.from({ length: Math.min(cols, 6) }).map((_, i) => (
          <div
            key={i}
            style={{
              height: 14,
              borderRadius: 4,
              background: "rgba(255,255,255,0.06)",
            }}
          />
        ))}
      </div>
      {Array.from({ length: 5 }).map((_, i) => (
        <div
          key={i}
          style={{
            display: "grid",
            gridTemplateColumns: `repeat(${Math.min(cols, 6)}, 1fr)`,
            gap: 12,
          }}
        >
          {Array.from({ length: Math.min(cols, 6) }).map((_, j) => (
            <div
              key={j}
              style={{
                height: 12,
                borderRadius: 4,
                background: "rgba(255,255,255,0.04)",
              }}
            />
          ))}
        </div>
      ))}
    </div>
  );
}

function EmptyState({ message }: { message: string }) {
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        height: "100%",
        color: "var(--text-muted)",
        fontSize: 14,
        gap: 12,
      }}
    >
      <Database size={40} style={{ opacity: 0.3 }} />
      <span>{message}</span>
    </div>
  );
}

export default function DataBrowserPage() {
  const [expandedApps, setExpandedApps] = useState<Set<string>>(new Set());
  const [selectedTable, setSelectedTable] = useState<{
    app: string;
    table: string;
    columns: DataBrowserTable["columns"];
  } | null>(null);
  const [pageOffset, setPageOffset] = useState(0);
  const [sortOrder, setSortOrder] = useState<string | undefined>(undefined);
  const [limit] = useState(50);
  const [isRefreshing, setIsRefreshing] = useState(false);

  const { data: apps, isLoading: appsLoading } = useDataBrowserApps();
  const {
    data: queryResult,
    isLoading: queryLoading,
    isFetching: queryFetching,
    refetch,
  } = useDataBrowserQuery(
    selectedTable?.app || "",
    selectedTable?.table || "",
    limit,
    pageOffset,
    sortOrder,
  );

  const toggleApp = (name: string) => {
    setExpandedApps((prev) => {
      const next = new Set(prev);
      if (next.has(name)) {
        next.delete(name);
      } else {
        next.add(name);
      }
      return next;
    });
  };

  const selectTable = (app: DataBrowserApp, table: DataBrowserTable) => {
    setSelectedTable({ app: app.name, table: table.name, columns: table.columns });
    setPageOffset(0);
    setSortOrder(undefined);
  };

  const handleSort = (col: string) => {
    setSortOrder((prev) => {
      if (!prev || prev !== `${col}.asc`) return `${col}.asc`;
      if (prev === `${col}.asc`) return `${col}.desc`;
      return undefined;
    });
    setPageOffset(0);
  };

  const getSortIcon = (col: string) => {
    if (!sortOrder || !sortOrder.startsWith(col)) return <ArrowUpDown size={12} style={{ opacity: 0.4 }} />;
    if (sortOrder === `${col}.asc`) return <ArrowUp size={12} />;
    return <ArrowDown size={12} />;
  };

  const handleRefresh = async () => {
    setIsRefreshing(true);
    await refetch();
    setIsRefreshing(false);
  };

  const columns = selectedTable?.columns || [];
  const data = queryResult?.data || [];
  const totalCount = queryResult?.count || 0;
  const totalPages = Math.max(1, Math.ceil(totalCount / limit));
  const currentPage = Math.floor(pageOffset / limit) + 1;

  return (
    <motion.div {...fadeUp} className="grid grid-cols-[240px_1fr] max-md:flex max-md:flex-col max-md:gap-3" style={{ minHeight: "100%" }}>
      {/* Tree panel */}
      <div
        className="max-md:max-h-[200px] max-md:overflow-y-auto"
        style={{
          background: "rgba(255,255,255,0.02)",
          borderRight: "1px solid rgba(255,255,255,0.06)",
          borderRadius: 16,
          overflow: "hidden",
          padding: 12,
        }}
      >
        <div
          style={{
            fontSize: 11,
            fontWeight: 600,
            textTransform: "uppercase",
            letterSpacing: "0.05em",
            color: "var(--text-muted)",
            padding: "8px 12px",
          }}
        >
          Apps
        </div>
        {appsLoading ? (
          <div style={{ padding: "12px", color: "var(--text-muted)", fontSize: 13 }}>
            Carregando...
          </div>
        ) : (
          <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>
            {(apps || []).map((app) => {
              const isExpanded = expandedApps.has(app.name);
              const isActive = selectedTable?.app === app.name;
              return (
                <div key={app.name}>
                  <button
                    onClick={() => toggleApp(app.name)}
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: 8,
                      width: "100%",
                      padding: "8px 12px",
                      borderRadius: 8,
                      border: "none",
                      background: isActive ? "rgba(255,255,255,0.06)" : "transparent",
                      color: "var(--text)",
                      fontSize: 13,
                      cursor: "pointer",
                      textAlign: "left",
                      transition: "background 0.15s",
                    }}
                    onMouseEnter={(e) => {
                      if (!isActive) e.currentTarget.style.background = "rgba(255,255,255,0.03)";
                    }}
                    onMouseLeave={(e) => {
                      if (!isActive) e.currentTarget.style.background = "transparent";
                    }}
                  >
                    {isExpanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                    <Database size={14} style={{ opacity: 0.6 }} />
                    <span style={{ fontWeight: 500, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                      {app.name}
                    </span>
                    <Badge
                      variant="secondary"
                      style={{
                        fontSize: 10,
                        padding: "0 6px",
                        marginLeft: "auto",
                        background: "rgba(255,255,255,0.06)",
                      }}
                    >
                      {app.tables.length}
                    </Badge>
                  </button>
                  {isExpanded && (
                    <div style={{ marginLeft: 8 }}>
                      {app.tables.map((table) => {
                        const isTableActive =
                          selectedTable?.app === app.name && selectedTable?.table === table.name;
                        return (
                          <button
                            key={table.name}
                            onClick={() => selectTable(app, table)}
                            style={{
                              display: "flex",
                              alignItems: "center",
                              gap: 8,
                              width: "100%",
                              padding: "6px 12px 6px 28px",
                              borderRadius: 6,
                              border: "none",
                              background: isTableActive ? "rgba(var(--brand-primary-rgb),0.12)" : "transparent",
                              color: isTableActive ? "var(--brand-primary)" : "var(--text-muted)",
                              fontSize: 13,
                              cursor: "pointer",
                              textAlign: "left",
                              transition: "all 0.15s",
                            }}
                            onMouseEnter={(e) => {
                              if (!isTableActive)
                                e.currentTarget.style.background = "rgba(255,255,255,0.03)";
                            }}
                            onMouseLeave={(e) => {
                              if (!isTableActive)
                                e.currentTarget.style.background = "transparent";
                            }}
                          >
                            <Table2 size={13} />
                            <span>{table.name}</span>
                          </button>
                        );
                      })}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* Data panel */}
      <div
        className="pl-4 max-md:pl-0 max-md:pt-3"
        style={{
          overflow: "hidden",
          display: "flex",
          flexDirection: "column",
        }}
      >
        {!selectedTable ? (
          <EmptyState message="Selecione uma tabela na árvore ao lado" />
        ) : (
          <>
            {/* Header */}
            <div
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                padding: "12px 16px",
                borderBottom: "1px solid rgba(255,255,255,0.06)",
              }}
            >
              <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                <Table2 size={16} style={{ opacity: 0.6 }} />
                <span style={{ fontSize: 14, fontWeight: 600 }}>
                  {selectedTable.app}.{selectedTable.table}
                </span>
                <span style={{ fontSize: 12, color: "var(--text-muted)" }}>
                  {columns.length} colunas
                </span>
              </div>
              <Button
                variant="ghost"
                size="sm"
                onClick={handleRefresh}
                disabled={isRefreshing || queryFetching}
                style={{ fontSize: 12 }}
              >
                <RefreshCw
                  size={14}
                  style={{
                    marginRight: 6,
                    animation: isRefreshing || queryFetching ? "spin 1s linear infinite" : undefined,
                  }}
                />
                Atualizar
              </Button>
            </div>

            {/* Desktop table */}
            <div className="max-md:hidden" style={{ overflow: "auto", flex: 1, position: "relative" }}>
              {queryLoading ? (
                <TableSkeleton cols={columns.length} />
              ) : (
                <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 13 }}>
                  <thead>
                    <tr
                      style={{
                        borderBottom: "1px solid rgba(255,255,255,0.06)",
                        position: "sticky",
                        top: 0,
                        background: "var(--bg)",
                        zIndex: 1,
                      }}
                    >
                      {columns.map((col) => (
                        <th
                          key={col.name}
                          onClick={() => handleSort(col.name)}
                          style={{
                            padding: "10px 12px",
                            textAlign: "left",
                            fontWeight: 500,
                            color: "var(--text-muted)",
                            fontSize: 12,
                            cursor: "pointer",
                            userSelect: "none",
                            whiteSpace: "nowrap",
                          }}
                        >
                          <div
                            style={{
                              display: "flex",
                              alignItems: "center",
                              gap: 4,
                            }}
                          >
                            <span>{col.name}</span>
                            {getSortIcon(col.name)}
                            <span
                              style={{
                                fontSize: 10,
                                opacity: 0.4,
                                marginLeft: 2,
                                fontFamily: "monospace",
                              }}
                            >
                              {col.type}
                            </span>
                          </div>
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {data.length === 0 ? (
                      <tr>
                        <td
                          colSpan={columns.length || 1}
                          style={{
                            padding: 40,
                            textAlign: "center",
                            color: "var(--text-muted)",
                            fontSize: 13,
                          }}
                        >
                          Nenhum registro encontrado
                        </td>
                      </tr>
                    ) : (
                      data.map((row, i) => (
                        <tr
                          key={i}
                          style={{
                            borderBottom: "1px solid rgba(255,255,255,0.04)",
                            transition: "background 0.1s",
                          }}
                          onMouseEnter={(e) => {
                            e.currentTarget.style.background = "rgba(255,255,255,0.02)";
                          }}
                          onMouseLeave={(e) => {
                            e.currentTarget.style.background = "transparent";
                          }}
                        >
                          {columns.map((col) => {
                            const val = row[col.name];
                            return (
                              <td
                                key={col.name}
                                style={{
                                  padding: "8px 12px",
                                  maxWidth: 250,
                                  overflow: "hidden",
                                  textOverflow: "ellipsis",
                                  whiteSpace: "nowrap",
                                  fontFamily:
                                    col.name === "id" ? "monospace" : undefined,
                                  fontSize: col.name === "id" ? 12 : 13,
                                  color:
                                    val === null || val === undefined
                                      ? "rgba(255,255,255,0.2)"
                                      : col.name === "id"
                                        ? "var(--text-muted)"
                                        : undefined,
                                  fontStyle:
                                    val === null || val === undefined
                                      ? "italic"
                                      : undefined,
                                }}
                              >
                                {formatCellValue(val, col.type)}
                              </td>
                            );
                          })}
                        </tr>
                      ))
                    )}
                  </tbody>
                </table>
              )}
            </div>

            {/* Mobile card view */}
            <div className="md:hidden flex-1 overflow-y-auto">
              {queryLoading ? (
                <TableSkeleton cols={columns.length} />
              ) : data.length === 0 ? (
                <div style={{ padding: 40, textAlign: "center", color: "var(--text-muted)", fontSize: 13 }}>
                  Nenhum registro encontrado
                </div>
              ) : (
                <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
                  {data.map((row, i) => (
                    <div
                      key={i}
                      style={{
                        background: "rgba(255,255,255,0.03)",
                        border: "1px solid rgba(255,255,255,0.06)",
                        borderRadius: 12,
                        overflow: "hidden",
                      }}
                    >
                      {columns.map((col, ci) => {
                        const val = row[col.name];
                        const isFirst = ci === 0;
                        return (
                          <div
                            key={col.name}
                            style={{
                              display: "flex",
                              alignItems: "flex-start",
                              gap: 8,
                              padding: isFirst ? "12px 14px 8px" : "6px 14px",
                              borderBottom: ci < columns.length - 1 ? "1px solid rgba(255,255,255,0.04)" : undefined,
                            }}
                          >
                            <span
                              style={{
                                fontSize: isFirst ? 10 : 11,
                                fontWeight: isFirst ? 700 : 500,
                                color: "var(--text-muted)",
                                minWidth: isFirst ? undefined : 90,
                                flexShrink: 0,
                                textTransform: isFirst ? "uppercase" : undefined,
                                letterSpacing: isFirst ? "0.05em" : undefined,
                              }}
                            >
                              {col.name}
                            </span>
                            <span
                              style={{
                                fontSize: isFirst ? 12 : 13,
                                fontWeight: isFirst ? 600 : 400,
                                color:
                                  val === null || val === undefined
                                    ? "rgba(255,255,255,0.2)"
                                    : isFirst
                                      ? "var(--text)"
                                      : undefined,
                                fontStyle:
                                  val === null || val === undefined
                                    ? "italic"
                                    : undefined,
                                fontFamily: col.name === "id" || isFirst ? "monospace" : undefined,
                                wordBreak: "break-word",
                                textAlign: "right",
                                flex: 1,
                              }}
                            >
                              {formatCellValue(val, col.type)}
                            </span>
                          </div>
                        );
                      })}
                    </div>
                  ))}
                </div>
              )}
            </div>

            {/* Pagination */}
            {totalCount > 0 && (
              <div
                style={{
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "space-between",
                  padding: "10px 16px",
                  borderTop: "1px solid rgba(255,255,255,0.06)",
                  fontSize: 12,
                  color: "var(--text-muted)",
                }}
              >
                <span>
                  {pageOffset + 1}–{Math.min(pageOffset + limit, totalCount)} de{" "}
                  {totalCount}
                </span>
                <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={pageOffset === 0}
                    onClick={() => setPageOffset(Math.max(0, pageOffset - limit))}
                    style={{ padding: "4px 8px", height: "auto" }}
                  >
                    <ChevronLeft size={14} />
                    Anterior
                  </Button>
                  <span style={{ padding: "0 8px", fontSize: 11 }}>
                    {currentPage} / {totalPages}
                  </span>
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={pageOffset + limit >= totalCount}
                    onClick={() => setPageOffset(pageOffset + limit)}
                    style={{ padding: "4px 8px", height: "auto" }}
                  >
                    Próximo
                    <ChevronRightIcon size={14} />
                  </Button>
                </div>
              </div>
            )}
          </>
        )}
      </div>

      <style>{`
        @keyframes spin {
          from { transform: rotate(0deg); }
          to { transform: rotate(360deg); }
        }
      `}</style>
    </motion.div>
  );
}

function formatCellValue(value: unknown, type: string): string {
  if (value === null || value === undefined) {
    return "NULL";
  }

  if (type === "boolean") {
    return value ? "true" : "false";
  }

  if (type === "timestamptz" && typeof value === "string") {
    try {
      return new Date(value).toLocaleString("pt-BR");
    } catch {
      return value;
    }
  }

  if (type === "jsonb") {
    try {
      return JSON.stringify(value);
    } catch {
      return String(value);
    }
  }

  return String(value);
}
