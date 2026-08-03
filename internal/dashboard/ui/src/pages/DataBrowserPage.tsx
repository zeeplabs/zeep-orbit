import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useDataBrowserApps,
  useDataBrowserQuery,
  useUpdateDataBrowserRow,
  useDeleteDataBrowserRow,
  exportDataBrowserCSV,
  DataBrowserApp,
  DataBrowserTable,
} from "../lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Icon } from "@/components/ui/icon";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  DataTable,
  EmptyState,
  ConfirmDialog,
  type Column,
  type SortDir,
} from "@/components/patterns";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

function filterOperators(t: (key: string) => string) {
  return [
    { value: "eq", label: "=" },
    { value: "ne", label: "≠" },
    { value: "gt", label: ">" },
    { value: "gte", label: ">=" },
    { value: "lt", label: "<" },
    { value: "lte", label: "<=" },
    { value: "ilike", label: t("dataBrowser.opContains") },
    { value: "like", label: "LIKE" },
  ];
}

const systemDisplayColumns = new Set(["id", "created_at", "updated_at", "owner_id"]);

const nativeInputCls =
  "w-full rounded-[10px] border border-[var(--border)] bg-[var(--sunken)] px-3 py-2 text-sm text-[var(--text-primary)] outline-none transition-colors focus:border-[var(--primary)]";

type Row = Record<string, unknown>;

export default function DataBrowserPage() {
  const { t } = useTranslation();
  const filterOps = filterOperators(t);
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
  const [editingRow, setEditingRow] = useState<Row | null>(null);
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
      if (next.has(name)) next.delete(name);
      else next.add(name);
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

  const handleRefresh = async () => {
    setIsRefreshing(true);
    await refetch();
    setIsRefreshing(false);
  };

  const openEditModal = (row: Row) => {
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
  const data: Row[] = queryResult?.data || [];
  const totalCount = queryResult?.count || 0;
  const totalPages = Math.max(1, Math.ceil(totalCount / limit));
  const currentPage = Math.floor(pageOffset / limit) + 1;
  const isSaving = updateRow.isPending;

  const sortProp = sortOrder
    ? { key: sortOrder.split(".")[0], dir: (sortOrder.endsWith(".desc") ? "desc" : "asc") as SortDir }
    : undefined;

  const tableColumns: Column<Row>[] = columns.map((col) => ({
    key: col.name,
    sortable: true,
    className: "max-w-[260px]",
    header: (
      <span className="inline-flex items-center gap-1.5">
        {col.name}
        <span className="font-mono text-[10px] font-normal opacity-50">{col.type}</span>
      </span>
    ),
    render: (row: Row) => {
      const val = row[col.name];
      const isNull = val === null || val === undefined;
      const isId = col.name === "id" || col.type === "uuid";
      return (
        <span
          className="block max-w-[260px] truncate"
          style={{
            fontFamily: isId ? "var(--font-mono)" : undefined,
            fontSize: isId ? 12 : undefined,
            color: isNull ? "var(--text-tertiary)" : col.name === "id" ? "var(--text-tertiary)" : undefined,
            fontStyle: isNull ? "italic" : undefined,
          }}
        >
          {formatCellValue(val, col.type)}
        </span>
      );
    },
  }));

  return (
    <div className="grid min-h-full grid-cols-[240px_1fr] gap-4 max-md:flex max-md:flex-col">
      {/* Tree panel */}
      <div className="overflow-hidden rounded-2xl border border-[var(--border)] bg-[var(--surface)] p-3 max-md:max-h-[220px] max-md:overflow-y-auto">
        <div className="px-3 py-2 text-[11px] font-semibold uppercase tracking-wide text-[var(--text-tertiary)]">
          {t("dataBrowser.appsLabel")}
        </div>
        {appsLoading ? (
          <div className="px-3 py-2 text-[13px] text-[var(--text-tertiary)]">{t("app.loading")}</div>
        ) : (
          <div className="flex flex-col gap-0.5">
            {(apps || []).map((app) => {
              const isExpanded = expandedApps.has(app.name);
              const isActive = selectedTable?.app === app.name;
              return (
                <div key={app.name}>
                  <button
                    onClick={() => toggleApp(app.name)}
                    className={cn(
                      "flex w-full cursor-pointer items-center gap-2 rounded-[8px] border-none px-3 py-2 text-left text-[13px] text-[var(--text-primary)] transition-colors hover:bg-[var(--hover-surface)]",
                      isActive ? "bg-[var(--hover-surface)]" : "bg-transparent",
                    )}
                  >
                    <Icon name={isExpanded ? "expand_more" : "chevron_right"} size={16} className="text-[var(--text-tertiary)]" />
                    <Icon name="database" size={15} className="text-[var(--text-tertiary)]" />
                    <span className="truncate font-medium">{app.name}</span>
                    <span className="ml-auto rounded-full bg-[var(--sunken)] px-1.5 py-0.5 text-[10px] text-[var(--text-tertiary)]">
                      {app.tables.length}
                    </span>
                  </button>
                  {isExpanded && (
                    <div className="ml-2 flex flex-col gap-0.5">
                      {app.tables.map((table) => {
                        const isTableActive =
                          selectedTable?.app === app.name && selectedTable?.table === table.name;
                        return (
                          <button
                            key={table.name}
                            onClick={() => selectTable(app, table)}
                            className={cn(
                              "flex w-full cursor-pointer items-center gap-2 rounded-md border-none py-1.5 pl-7 pr-3 text-left text-[13px] transition-colors",
                              isTableActive
                                ? "bg-[var(--primary-tint)] text-[var(--primary)]"
                                : "bg-transparent text-[var(--text-secondary)] hover:bg-[var(--hover-surface)]",
                            )}
                          >
                            <Icon name="table_chart" size={14} />
                            <span className="truncate">{table.name}</span>
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
      <div className="flex min-w-0 flex-col gap-3">
        {!selectedTable ? (
          <EmptyState icon="database" title={t("dataBrowser.emptySelect")} />
        ) : (
          <>
            {/* Toolbar */}
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="flex items-center gap-2 text-[var(--text-primary)]">
                <Icon name="table_chart" size={16} className="text-[var(--text-tertiary)]" />
                <span className="text-sm font-semibold">
                  {selectedTable.app}.{selectedTable.table}
                </span>
                <span className="text-xs text-[var(--text-tertiary)]">
                  {t("dataBrowser.columnsCount", { count: columns.length })}
                </span>
              </div>
              <div className="flex items-center gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  className={cn("gap-1.5", showFilters && "border-[var(--primary)] text-[var(--primary)]")}
                  onClick={() => setShowFilters((v) => !v)}
                >
                  <Icon name="filter_list" size={15} />
                  {t("dataBrowser.filters")}
                  {activeFilterCount > 0 && (
                    <span className="ml-0.5 rounded-full bg-[var(--primary)] px-1.5 text-[10px] leading-4 text-white">
                      {activeFilterCount}
                    </span>
                  )}
                </Button>
                <Button variant="outline" size="sm" className="gap-1.5" onClick={handleExport} disabled={isExporting}>
                  <Icon name={isExporting ? "progress_activity" : "download"} size={15} className={isExporting ? "animate-spin" : undefined} />
                  CSV
                </Button>
                <Button variant="outline" size="sm" className="gap-1.5" onClick={handleRefresh} disabled={isRefreshing || queryFetching}>
                  <Icon name="refresh" size={15} className={isRefreshing || queryFetching ? "animate-spin" : undefined} />
                  {t("dataBrowser.refresh")}
                </Button>
              </div>
            </div>

            {/* Filter panel */}
            {showFilters && columns.length > 0 && (
              <div className="flex flex-col gap-3 rounded-2xl border border-[var(--border)] bg-[var(--surface)] p-4">
                <div className="flex flex-wrap items-center gap-2">
                  <Select value={draftCol} onValueChange={setDraftCol}>
                    <SelectTrigger className="h-9 min-w-[150px]">
                      <SelectValue placeholder={t("dataBrowser.columnPlaceholder")} />
                    </SelectTrigger>
                    <SelectContent>
                      {columns.map((col) => (
                        <SelectItem key={col.name} value={col.name}>{col.name}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Select value={draftOp} onValueChange={setDraftOp}>
                    <SelectTrigger className="h-9 w-auto min-w-[90px]">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {filterOps.map((op) => (
                        <SelectItem key={op.value} value={op.value}>{op.label}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Input
                    value={draftValue}
                    onChange={(e) => setDraftValue(e.target.value)}
                    onKeyDown={(e) => { if (e.key === "Enter") addFilterRule(); }}
                    placeholder={t("dataBrowser.valuePlaceholder")}
                    className="h-9 max-w-[220px] flex-1"
                  />
                  <Button size="sm" className="gap-1" onClick={addFilterRule} disabled={!draftCol || !draftValue.trim()}>
                    <Icon name="add" size={15} />
                    {t("dataBrowser.addFilter")}
                  </Button>
                </div>

                {filterRules.length > 0 && (
                  <div className="flex flex-wrap items-center gap-2">
                    {filterRules.map((r) => (
                      <div
                        key={r.col}
                        className="flex items-center gap-1.5 rounded-full border border-[var(--primary)]/25 bg-[var(--primary-tint)] py-1 pl-2.5 pr-2 text-xs"
                      >
                        <span className="font-semibold text-[var(--primary)]">{r.col}</span>
                        <span className="text-[var(--text-tertiary)]">{filterOps.find((o) => o.value === r.op)?.label}</span>
                        <span className="text-[var(--text-primary)]">{r.value}</span>
                        <button
                          title={t("dataBrowser.removeFilter")}
                          onClick={() => removeFilterRule(r.col)}
                          className="flex cursor-pointer items-center border-none bg-transparent text-[var(--text-tertiary)] hover:text-[var(--text-primary)]"
                        >
                          <Icon name="close" size={13} />
                        </button>
                      </div>
                    ))}
                    <button
                      onClick={clearFilters}
                      className="cursor-pointer border-none bg-transparent px-1.5 py-0.5 text-[11px] text-[var(--danger)]"
                    >
                      {t("dataBrowser.clearFilters")}
                    </button>
                  </div>
                )}
              </div>
            )}

            <DataTable<Row>
              columns={tableColumns}
              rows={data}
              getRowId={(row) => String(row["id"])}
              loading={queryLoading}
              empty={{ icon: "table_rows", title: t("dataBrowser.noRecords") }}
              sort={sortProp}
              onSort={handleSort}
              pagination={{
                page: currentPage,
                pageCount: totalPages,
                onPageChange: (p) => setPageOffset((p - 1) * limit),
                prevLabel: t("dataBrowser.previous"),
                nextLabel: t("dataBrowser.next"),
                label: t("dataBrowser.pageRange", {
                  from: pageOffset + 1,
                  to: Math.min(pageOffset + limit, totalCount),
                  total: totalCount,
                }),
              }}
              rowActions={(row) => (
                <>
                  <Button variant="outline" size="icon" className="size-8" title={t("dataBrowser.edit")} onClick={() => openEditModal(row)}>
                    <Icon name="edit" size={15} />
                  </Button>
                  <Button
                    variant="outline"
                    size="icon"
                    className="size-8 border-[var(--danger)]/30 text-[var(--danger)] hover:bg-[var(--danger-tint)]"
                    title={t("dataBrowser.delete")}
                    onClick={() => setDeleteConfirmId(String(row["id"]))}
                  >
                    <Icon name="delete" size={15} />
                  </Button>
                </>
              )}
            />
          </>
        )}
      </div>

      {/* Edit Modal */}
      <Dialog open={modalOpen && !!selectedTable} onOpenChange={(o) => { if (!o) closeModal(); }}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{t("dataBrowser.editRecordTitle")}</DialogTitle>
          </DialogHeader>
          <div className="flex max-h-[60vh] flex-col gap-3.5 overflow-auto">
            {columns
              .filter((col) => !systemDisplayColumns.has(col.name))
              .map((col) => (
                <div key={col.name} className="flex flex-col gap-1.5">
                  <Label className="flex items-center gap-1.5">
                    {col.name}
                    <span className="font-mono text-[10px] font-normal opacity-50">{col.type}</span>
                  </Label>
                  {col.type === "boolean" ? (
                    <select
                      value={formValues[col.name] ?? ""}
                      onChange={(e) => handleFormChange(col.name, e.target.value)}
                      className={nativeInputCls}
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
                      className={cn(nativeInputCls, "resize-y font-mono")}
                    />
                  ) : (
                    <Input
                      type={col.type === "integer" || col.type === "bigint" || col.type === "decimal" || col.type === "numeric" ? "number" : "text"}
                      value={formValues[col.name] ?? ""}
                      onChange={(e) => handleFormChange(col.name, e.target.value)}
                      placeholder={col.name}
                      className={col.type === "uuid" ? "font-mono" : undefined}
                    />
                  )}
                </div>
              ))}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={closeModal} disabled={isSaving}>
              {t("appForm.cancel")}
            </Button>
            <Button onClick={handleSave} disabled={isSaving} className="gap-2">
              {isSaving && <Icon name="progress_activity" size={16} className="animate-spin" />}
              {isSaving ? t("dataBrowser.saving") : t("dataBrowser.save")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete confirmation */}
      <ConfirmDialog
        open={!!deleteConfirmId}
        title={t("dataBrowser.deleteRecordTitle")}
        message={t("dataBrowser.deleteConfirmMsg")}
        confirmLabel={deleteRow.isPending ? t("dataBrowser.deleting") : t("dataBrowser.delete")}
        cancelLabel={t("appForm.cancel")}
        destructive
        icon="delete"
        loading={deleteRow.isPending}
        onConfirm={() => deleteConfirmId && handleDelete(deleteConfirmId)}
        onCancel={() => setDeleteConfirmId(null)}
      />
    </div>
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
