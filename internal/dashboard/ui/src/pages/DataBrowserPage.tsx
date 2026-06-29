import { useState } from "react";
import { useTranslation } from "react-i18next";
import { motion, AnimatePresence } from "framer-motion";
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
  Pencil,
  Trash2,
  X,
  Filter,
  Download,
} from "lucide-react";
import {
  useDataBrowserApps,
  useDataBrowserQuery,
  useUpdateDataBrowserRow,
  useDeleteDataBrowserRow,
  exportDataBrowserCSV,
  DataBrowserApp,
  DataBrowserTable,
} from "../lib/api";

const FILTER_OPERATORS = [
  { value: "eq", label: "=" },
  { value: "ne", label: "≠" },
  { value: "gt", label: ">" },
  { value: "gte", label: ">=" },
  { value: "lt", label: "<" },
  { value: "lte", label: "<=" },
  { value: "ilike", label: "contém" },
  { value: "like", label: "LIKE" },
];
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";

const ease = [0.32, 0.72, 0, 1] as const;

const systemDisplayColumns = new Set(["id", "created_at", "updated_at", "owner_id"]);

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
  const { t } = useTranslation();
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
  const [showFilters, setShowFilters] = useState(false);
  const [filterRules, setFilterRules] = useState<Array<{ col: string; op: string; value: string }>>([]);
  const [draftCol, setDraftCol] = useState("");
  const [draftOp, setDraftOp] = useState("eq");
  const [draftValue, setDraftValue] = useState("");
  const [isExporting, setIsExporting] = useState(false);

  const [modalOpen, setModalOpen] = useState(false);
  const [editingRow, setEditingRow] = useState<Record<string, unknown> | null>(null);
  const [formValues, setFormValues] = useState<Record<string, string>>({});
  const [deleteConfirmId, setDeleteConfirmId] = useState<string | null>(null);

  const activeFilters: Record<string, string> = {};
  for (const r of filterRules) {
    if (r.value.trim()) activeFilters[r.col] = `${r.op}.${r.value.trim()}`;
  }
  const activeFilterCount = filterRules.length;

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
    activeFilterCount > 0 ? activeFilters : undefined,
  );

  const updateRow = useUpdateDataBrowserRow();
  const deleteRow = useDeleteDataBrowserRow();

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
    setFilterRules([]);
    setDraftCol("");
    setDraftValue("");
  };

  const addFilterRule = () => {
    if (!draftCol || !draftValue.trim()) return;
    setFilterRules((prev) => [
      ...prev.filter((r) => r.col !== draftCol),
      { col: draftCol, op: draftOp, value: draftValue.trim() },
    ]);
    setDraftValue("");
    setPageOffset(0);
  };

  const removeFilterRule = (col: string) => {
    setFilterRules((prev) => prev.filter((r) => r.col !== col));
    setPageOffset(0);
  };

  const clearFilters = () => {
    setFilterRules([]);
    setPageOffset(0);
  };

  const handleExport = async () => {
    if (!selectedTable) return;
    setIsExporting(true);
    try {
      await exportDataBrowserCSV(
        selectedTable.app,
        selectedTable.table,
        activeFilterCount > 0 ? activeFilters : undefined,
      );
    } finally {
      setIsExporting(false);
    }
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


  const openEditModal = (row: Record<string, unknown>) => {
    setEditingRow(row);
    const initial: Record<string, string> = {};
    for (const col of columns) {
      if (!systemDisplayColumns.has(col.name)) {
        initial[col.name] = row[col.name] != null ? String(row[col.name]) : "";
      }
    }
    setFormValues(initial);
    setModalOpen(true);
  };

  const closeModal = () => {
    setModalOpen(false);
    setEditingRow(null);
    setFormValues({});
  };

  const handleFormChange = (colName: string, value: string) => {
    setFormValues((prev) => ({ ...prev, [colName]: value }));
  };

  const handleSave = async () => {
    if (!selectedTable) return;

    const data: Record<string, unknown> = {};
    for (const col of columns) {
      if (systemDisplayColumns.has(col.name)) continue;
      const val = formValues[col.name];
      const colDef = columns.find((c) => c.name === col.name);
      if (!colDef) continue;

      if (val === "") {
        if (colDef.type === "boolean") {
          data[col.name] = false;
        } else {
          data[col.name] = null;
        }
      } else if (colDef.type === "integer" || colDef.type === "bigint") {
        data[col.name] = parseInt(val, 10);
      } else if (colDef.type === "decimal" || colDef.type === "numeric") {
        data[col.name] = parseFloat(val);
      } else if (colDef.type === "boolean") {
        data[col.name] = val === "true";
      } else {
        data[col.name] = val;
      }
    }

    try {
      if (editingRow) {
        await updateRow.mutateAsync({
          app: selectedTable.app,
          table: selectedTable.table,
          id: String(editingRow["id"]),
          data,
        });
      }
      closeModal();
    } catch {
    }
  };

  const handleDelete = async (id: string) => {
    if (!selectedTable) return;
    try {
      await deleteRow.mutateAsync({
        app: selectedTable.app,
        table: selectedTable.table,
        id,
      });
      setDeleteConfirmId(null);
    } catch {
      setDeleteConfirmId(null);
    }
  };

  const columns = selectedTable?.columns || [];
  const data = queryResult?.data || [];
  const totalCount = queryResult?.count || 0;
  const totalPages = Math.max(1, Math.ceil(totalCount / limit));
  const currentPage = Math.floor(pageOffset / limit) + 1;

  const isSaving = updateRow.isPending;

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
          <EmptyState message={t("dataBrowser.emptySelect")} />
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
              <div style={{ display: "flex", gap: 6 }}>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setShowFilters((v) => !v)}
                  style={{
                    fontSize: 12,
                    background: showFilters ? "rgba(var(--brand-primary-rgb),0.12)" : undefined,
                    color: showFilters ? "var(--brand-primary)" : undefined,
                  }}
                >
                  <Filter size={14} style={{ marginRight: 6 }} />
                  Filtros
                  {activeFilterCount > 0 && (
                    <span
                      style={{
                        marginLeft: 6,
                        background: "var(--brand-primary)",
                        color: "#fff",
                        borderRadius: 999,
                        fontSize: 10,
                        padding: "0 5px",
                        lineHeight: "16px",
                      }}
                    >
                      {activeFilterCount}
                    </span>
                  )}
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={handleExport}
                  disabled={isExporting}
                  style={{ fontSize: 12 }}
                >
                  {isExporting ? (
                    <Loader2 size={14} style={{ marginRight: 6, animation: "spin 1s linear infinite" }} />
                  ) : (
                    <Download size={14} style={{ marginRight: 6 }} />
                  )}
                  CSV
                </Button>
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
            </div>

            {/* Filter panel */}
            {showFilters && columns.length > 0 && (
              <div
                style={{
                  padding: "10px 16px",
                  borderBottom: "1px solid rgba(255,255,255,0.06)",
                  display: "flex",
                  flexDirection: "column",
                  gap: 8,
                }}
              >
                {/* Add rule row */}
                <div style={{ display: "flex", gap: 6, alignItems: "center", flexWrap: "wrap" }}>
                  <select
                    value={draftCol}
                    onChange={(e) => setDraftCol(e.target.value)}
                    style={{
                      padding: "5px 8px",
                      borderRadius: 6,
                      border: "1px solid rgba(255,255,255,0.1)",
                      background: "rgba(255,255,255,0.03)",
                      color: draftCol ? "var(--text)" : "var(--text-muted)",
                      fontSize: 12,
                      cursor: "pointer",
                      minWidth: 130,
                    }}
                  >
                    <option value="">Column...</option>
                    {columns.map((col) => (
                      <option key={col.name} value={col.name}>{col.name}</option>
                    ))}
                  </select>
                  <select
                    value={draftOp}
                    onChange={(e) => setDraftOp(e.target.value)}
                    style={{
                      padding: "5px 8px",
                      borderRadius: 6,
                      border: "1px solid rgba(255,255,255,0.1)",
                      background: "rgba(255,255,255,0.03)",
                      color: "var(--text)",
                      fontSize: 12,
                      cursor: "pointer",
                    }}
                  >
                    {FILTER_OPERATORS.map((op) => (
                      <option key={op.value} value={op.value}>{op.label}</option>
                    ))}
                  </select>
                  <input
                    type="text"
                    value={draftValue}
                    onChange={(e) => setDraftValue(e.target.value)}
                    onKeyDown={(e) => { if (e.key === "Enter") addFilterRule(); }}
                    placeholder="valor"
                    style={{
                      padding: "5px 10px",
                      borderRadius: 6,
                      border: "1px solid rgba(255,255,255,0.1)",
                      background: "rgba(255,255,255,0.03)",
                      color: "var(--text)",
                      fontSize: 12,
                      outline: "none",
                      flex: 1,
                      minWidth: 120,
                      maxWidth: 220,
                    }}
                  />
                  <Button
                    size="sm"
                    onClick={addFilterRule}
                    disabled={!draftCol || !draftValue.trim()}
                    style={{ fontSize: 12 }}
                  >
                    + Adicionar
                  </Button>
                </div>

                {/* Active filter chips */}
                {filterRules.length > 0 && (
                  <div style={{ display: "flex", flexWrap: "wrap", gap: 6, alignItems: "center" }}>
                    {filterRules.map((r) => (
                      <div
                        key={r.col}
                        style={{
                          display: "flex",
                          alignItems: "center",
                          gap: 4,
                          background: "rgba(var(--brand-primary-rgb),0.1)",
                          border: "1px solid rgba(var(--brand-primary-rgb),0.2)",
                          borderRadius: 999,
                          padding: "3px 8px 3px 10px",
                          fontSize: 12,
                        }}
                      >
                        <span style={{ fontWeight: 600, color: "var(--brand-primary)" }}>{r.col}</span>
                        <span style={{ color: "var(--text-muted)", margin: "0 1px" }}>
                          {FILTER_OPERATORS.find((o) => o.value === r.op)?.label}
                        </span>
                        <span style={{ color: "var(--text)" }}>{r.value}</span>
                        <button
                          onClick={() => removeFilterRule(r.col)}
                          style={{
                            marginLeft: 4,
                            border: "none",
                            background: "transparent",
                            color: "var(--text-muted)",
                            cursor: "pointer",
                            padding: 0,
                            display: "flex",
                            alignItems: "center",
                          }}
                        >
                          <X size={12} />
                        </button>
                      </div>
                    ))}
                    <button
                      onClick={clearFilters}
                      style={{
                        fontSize: 11,
                        color: "#ef4444",
                        background: "transparent",
                        border: "none",
                        cursor: "pointer",
                        padding: "2px 6px",
                      }}
                    >
                      Limpar tudo
                    </button>
                  </div>
                )}
              </div>
            )}

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
                      <th
                        style={{
                          padding: "10px 12px",
                          width: 80,
                          textAlign: "center",
                          fontWeight: 500,
                          color: "var(--text-muted)",
                          fontSize: 12,
                        }}
                      >
                        Ações
                      </th>
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
                          colSpan={columns.length + 1}
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
                          key={row["id"] as string || i}
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
                          <td style={{ padding: "8px 12px", textAlign: "center" }}>
                            <div style={{ display: "flex", gap: 4, justifyContent: "center" }}>
                              <button
                                onClick={() => openEditModal(row)}
                                style={{
                                  padding: 4,
                                  border: "none",
                                  background: "transparent",
                                  color: "var(--text-muted)",
                                  cursor: "pointer",
                                  borderRadius: 4,
                                  transition: "all 0.15s",
                                }}
                                title={t("dataBrowser.edit")}
                                onMouseEnter={(e) => {
                                  e.currentTarget.style.background = "rgba(255,255,255,0.06)";
                                  e.currentTarget.style.color = "var(--brand-primary)";
                                }}
                                onMouseLeave={(e) => {
                                  e.currentTarget.style.background = "transparent";
                                  e.currentTarget.style.color = "var(--text-muted)";
                                }}
                              >
                                <Pencil size={14} />
                              </button>
                              <button
                                onClick={() => setDeleteConfirmId(String(row["id"]))}
                                style={{
                                  padding: 4,
                                  border: "none",
                                  background: "transparent",
                                  color: "var(--text-muted)",
                                  cursor: "pointer",
                                  borderRadius: 4,
                                  transition: "all 0.15s",
                                }}
                                title={t("dataBrowser.delete")}
                                onMouseEnter={(e) => {
                                  e.currentTarget.style.background = "rgba(255,255,255,0.06)";
                                  e.currentTarget.style.color = "#ef4444";
                                }}
                                onMouseLeave={(e) => {
                                  e.currentTarget.style.background = "transparent";
                                  e.currentTarget.style.color = "var(--text-muted)";
                                }}
                              >
                                <Trash2 size={14} />
                              </button>
                            </div>
                          </td>
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
                      key={row["id"] as string || i}
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
                      {/* Mobile action buttons */}
                      <div
                        style={{
                          display: "flex",
                          gap: 4,
                          padding: "8px 14px",
                          borderTop: "1px solid rgba(255,255,255,0.04)",
                        }}
                      >
                        <button
                          onClick={() => openEditModal(row)}
                          style={{
                            flex: 1,
                            padding: "6px 12px",
                            border: "1px solid rgba(255,255,255,0.1)",
                            background: "transparent",
                            color: "var(--text-muted)",
                            cursor: "pointer",
                            borderRadius: 6,
                            fontSize: 12,
                            display: "flex",
                            alignItems: "center",
                            justifyContent: "center",
                            gap: 4,
                          }}
                        >
                          <Pencil size={12} />
                          Editar
                        </button>
                        <button
                          onClick={() => setDeleteConfirmId(String(row["id"]))}
                          style={{
                            flex: 1,
                            padding: "6px 12px",
                            border: "1px solid rgba(239,68,68,0.3)",
                            background: "transparent",
                            color: "#ef4444",
                            cursor: "pointer",
                            borderRadius: 6,
                            fontSize: 12,
                            display: "flex",
                            alignItems: "center",
                            justifyContent: "center",
                            gap: 4,
                          }}
                        >
                          <Trash2 size={12} />
                          Excluir
                        </button>
                      </div>
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

      {/* ── Create/Edit Modal ── */}
      <AnimatePresence>
        {modalOpen && selectedTable && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            style={{
              position: "fixed",
              inset: 0,
              zIndex: 100,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              background: "rgba(0,0,0,0.6)",
              backdropFilter: "blur(4px)",
              padding: 16,
            }}
            onClick={(e) => {
              if (e.target === e.currentTarget) closeModal();
            }}
          >
            <motion.div
              initial={{ opacity: 0, scale: 0.95, y: 20 }}
              animate={{ opacity: 1, scale: 1, y: 0 }}
              exit={{ opacity: 0, scale: 0.95, y: 20 }}
              transition={{ duration: 0.2, ease }}
              style={{
                width: "100%",
                maxWidth: 520,
                maxHeight: "85vh",
                overflow: "hidden",
                display: "flex",
                flexDirection: "column",
                background: "var(--bg-card, #1a1a2e)",
                border: "1px solid rgba(255,255,255,0.1)",
                borderRadius: 16,
              }}
            >
              {/* Modal header */}
              <div
                style={{
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "space-between",
                  padding: "16px 20px",
                  borderBottom: "1px solid rgba(255,255,255,0.06)",
                }}
              >
                <span style={{ fontSize: 15, fontWeight: 600 }}>
                  Editar registro
                </span>
                <button
                  onClick={closeModal}
                  style={{
                    padding: 4,
                    border: "none",
                    background: "transparent",
                    color: "var(--text-muted)",
                    cursor: "pointer",
                    borderRadius: 6,
                  }}
                >
                  <X size={18} />
                </button>
              </div>

              {/* Modal body */}
              <div
                style={{
                  padding: "16px 20px",
                  overflow: "auto",
                  flex: 1,
                  display: "flex",
                  flexDirection: "column",
                  gap: 14,
                }}
              >
                {columns
                  .filter((col) => !systemDisplayColumns.has(col.name))
                  .map((col) => (
                    <div key={col.name} style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                      <label
                        style={{
                          fontSize: 12,
                          fontWeight: 500,
                          color: "var(--text-muted)",
                          display: "flex",
                          alignItems: "center",
                          gap: 4,
                        }}
                      >
                        {col.name}
                        <span style={{ fontSize: 10, fontFamily: "monospace", opacity: 0.4 }}>
                          {col.type}
                        </span>
                      </label>
                      {col.type === "boolean" ? (
                        <select
                          value={formValues[col.name] ?? ""}
                          onChange={(e) => handleFormChange(col.name, e.target.value)}
                          style={{
                            padding: "8px 12px",
                            borderRadius: 8,
                            border: "1px solid rgba(255,255,255,0.1)",
                            background: "rgba(255,255,255,0.03)",
                            color: "var(--text)",
                            fontSize: 13,
                            fontFamily: "inherit",
                            outline: "none",
                          }}
                        >
                          <option value="">—</option>
                          <option value="true">true</option>
                          <option value="false">false</option>
                        </select>
                      ) : col.type === "jsonb" ? (
                        <textarea
                          value={formValues[col.name] ?? ""}
                          onChange={(e) => handleFormChange(col.name, e.target.value)}
                          rows={3}
                          style={{
                            padding: "8px 12px",
                            borderRadius: 8,
                            border: "1px solid rgba(255,255,255,0.1)",
                            background: "rgba(255,255,255,0.03)",
                            color: "var(--text)",
                            fontSize: 13,
                            fontFamily: "monospace",
                            outline: "none",
                            resize: "vertical",
                          }}
                        />
                      ) : (
                        <input
                          type={col.type === "integer" || col.type === "bigint" || col.type === "decimal" || col.type === "numeric" ? "number" : "text"}
                          value={formValues[col.name] ?? ""}
                          onChange={(e) => handleFormChange(col.name, e.target.value)}
                          placeholder={col.name}
                          style={{
                            padding: "8px 12px",
                            borderRadius: 8,
                            border: "1px solid rgba(255,255,255,0.1)",
                            background: "rgba(255,255,255,0.03)",
                            color: "var(--text)",
                            fontSize: 13,
                            fontFamily: col.type === "uuid" ? "monospace" : "inherit",
                            outline: "none",
                          }}
                        />
                      )}
                    </div>
                  ))}
              </div>

              {/* Modal footer */}
              <div
                style={{
                  display: "flex",
                  justifyContent: "flex-end",
                  gap: 8,
                  padding: "12px 20px",
                  borderTop: "1px solid rgba(255,255,255,0.06)",
                }}
              >
                <Button variant="ghost" size="sm" onClick={closeModal} disabled={isSaving}>
                  Cancelar
                </Button>
                <Button size="sm" onClick={handleSave} disabled={isSaving}>
                  {isSaving ? (
                    <>
                      <Loader2 size={14} style={{ marginRight: 6, animation: "spin 1s linear infinite" }} />
                      Saving...
                    </>
                  ) : (
                    t("dataBrowser.save")
                  )}
                </Button>
              </div>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>

      {/* ── Delete Confirmation Modal ── */}
      <AnimatePresence>
        {deleteConfirmId && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            style={{
              position: "fixed",
              inset: 0,
              zIndex: 110,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              background: "rgba(0,0,0,0.6)",
              backdropFilter: "blur(4px)",
              padding: 16,
            }}
            onClick={(e) => {
              if (e.target === e.currentTarget) setDeleteConfirmId(null);
            }}
          >
            <motion.div
              initial={{ opacity: 0, scale: 0.95, y: 20 }}
              animate={{ opacity: 1, scale: 1, y: 0 }}
              exit={{ opacity: 0, scale: 0.95, y: 20 }}
              transition={{ duration: 0.2, ease }}
              style={{
                width: "100%",
                maxWidth: 400,
                background: "var(--bg-card, #1a1a2e)",
                border: "1px solid rgba(255,255,255,0.1)",
                borderRadius: 16,
                overflow: "hidden",
              }}
            >
              <div
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 12,
                  padding: "20px 20px 0",
                }}
              >
                <div
                  style={{
                    width: 40,
                    height: 40,
                    borderRadius: "50%",
                    background: "rgba(239,68,68,0.15)",
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                    flexShrink: 0,
                  }}
                >
                  <Trash2 size={18} style={{ color: "#ef4444" }} />
                </div>
                <div>
                  <div style={{ fontSize: 15, fontWeight: 600, marginBottom: 4 }}>
                    Excluir registro
                  </div>
                  <div style={{ fontSize: 13, color: "var(--text-muted)", lineHeight: 1.4 }}>
                    Tem certeza que deseja excluir este registro? Esta ação não pode ser desfeita.
                  </div>
                </div>
              </div>
              <div
                style={{
                  display: "flex",
                  justifyContent: "flex-end",
                  gap: 8,
                  padding: "16px 20px",
                  marginTop: 8,
                }}
              >
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setDeleteConfirmId(null)}
                  disabled={deleteRow.isPending}
                >
                  Cancelar
                </Button>
                <Button
                  size="sm"
                  onClick={() => handleDelete(deleteConfirmId)}
                  disabled={deleteRow.isPending}
                  style={{
                    background: "#ef4444",
                    color: "#fff",
                    border: "none",
                  }}
                >
                  {deleteRow.isPending ? (
                    <>
                      <Loader2 size={14} style={{ marginRight: 6, animation: "spin 1s linear infinite" }} />
                      Excluindo...
                    </>
                  ) : (
                    <>
                      <Trash2 size={14} style={{ marginRight: 6 }} />
                      Excluir
                    </>
                  )}
                </Button>
              </div>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>

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
