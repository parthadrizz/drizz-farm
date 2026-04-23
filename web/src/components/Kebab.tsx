/**
 * Kebab — three-dot dropdown for row-level actions.
 *
 * Used on lists where actions vary per row but you don't want a wide
 * action column eating horizontal space. Click the dots, get a small
 * popover of menu items, click outside to dismiss.
 */
import { useEffect, useRef, useState, ReactNode } from 'react';
import { MoreVertical, LucideIcon } from 'lucide-react';

export interface KebabItem {
  label: string;
  icon?: LucideIcon;
  onClick: () => void;
  /** Tints the item in destructive red — confirm with the caller before invoking. */
  danger?: boolean;
  disabled?: boolean;
}

export function Kebab({ items, label = 'More actions' }: { items: KebabItem[]; label?: string }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', onDoc);
    return () => document.removeEventListener('mousedown', onDoc);
  }, [open]);

  return (
    <div className="relative" ref={ref}>
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          e.preventDefault();
          setOpen((v) => !v);
        }}
        className="p-1.5 rounded hover:bg-surface-2 text-muted-foreground hover:text-foreground transition-colors"
        title={label}
        aria-label={label}
      >
        <MoreVertical className="w-3.5 h-3.5" />
      </button>
      {open && (
        <div
          onClick={(e) => e.stopPropagation()}
          className="absolute right-0 top-full mt-1 z-30 min-w-[180px] surface-1 border border-border rounded-lg shadow-xl py-1 animate-fade-in"
        >
          {items.map((it, i) => (
            <button
              key={i}
              disabled={it.disabled}
              onClick={() => {
                setOpen(false);
                it.onClick();
              }}
              className={`w-full text-left px-3 py-2 text-xs flex items-center gap-2 transition-colors disabled:opacity-40 disabled:cursor-not-allowed ${
                it.danger
                  ? 'text-destructive hover:bg-destructive/10'
                  : 'text-foreground hover:bg-surface-2'
              }`}
            >
              {it.icon && <it.icon className="w-3.5 h-3.5" />}
              <span>{it.label}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

/**
 * ConfirmDialog — minimal modal for destructive confirmations.
 * Headless: caller controls open state.
 */
export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel = 'Confirm',
  cancelLabel = 'Cancel',
  danger,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  title: string;
  description?: ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  if (!open) return null;
  return (
    <div
      onClick={onCancel}
      className="fixed inset-0 z-50 flex items-center justify-center p-4"
      style={{ background: 'hsl(var(--surface-0) / 0.7)' }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="surface-1 border border-border rounded-xl shadow-2xl max-w-md w-full p-6 animate-fade-in"
      >
        <h2 className="text-base font-semibold text-foreground mb-2">{title}</h2>
        {description && <div className="text-sm text-muted-foreground leading-relaxed mb-5">{description}</div>}
        <div className="flex items-center justify-end gap-3">
          <button onClick={onCancel} className="action-btn surface-3 text-foreground">
            {cancelLabel}
          </button>
          <button
            onClick={onConfirm}
            className={`action-btn ${danger ? 'action-btn-danger' : 'action-btn-primary'}`}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
