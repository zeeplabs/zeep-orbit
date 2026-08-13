interface RoleChipPickerProps {
  availableRoles: string[];
  selected: string[];
  onToggle: (role: string) => void;
  label: string;
  placeholder: string;
  hint?: string;
}

// Extracted 1:1 from TablePolicies.tsx's chipRoles/toggleRole block — same
// orphaned-role behavior (ROLECFG-16): a role already selected but no
// longer in availableRoles still renders as a selected chip, never silently
// dropped.
export function RoleChipPicker({ availableRoles, selected, onToggle, label, placeholder, hint }: RoleChipPickerProps) {
  const chipRoles = Array.from(new Set([...availableRoles, ...selected]));

  return (
    <div className="flex flex-wrap items-center gap-1.5" title={hint}>
      <span className="text-[11px] font-semibold uppercase tracking-wide text-[var(--text-tertiary)]">
        {label}
      </span>
      {chipRoles.length === 0 ? (
        <span className="text-[12px] text-[var(--text-tertiary)]">{placeholder}</span>
      ) : (
        chipRoles.map((role) => {
          const isSelected = selected.includes(role);
          return (
            <button
              key={role}
              type="button"
              onClick={() => onToggle(role)}
              className={
                isSelected
                  ? "rounded-full border border-[var(--primary)] bg-[var(--primary)] px-2.5 py-1 text-[12px] font-semibold text-white cursor-pointer"
                  : "rounded-full border border-[var(--border)] bg-[var(--surface)] px-2.5 py-1 text-[12px] font-semibold text-[var(--text-secondary)] cursor-pointer hover:bg-[var(--hover-surface)]"
              }
            >
              {role}
            </button>
          );
        })
      )}
    </div>
  );
}
