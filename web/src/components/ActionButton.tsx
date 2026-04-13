import { ReactNode } from 'react';

interface ActionButtonProps {
  onClick: () => void;
  loading?: boolean;
  disabled?: boolean;
  variant?: 'primary' | 'accent' | 'danger' | 'warning';
  children: ReactNode;
  className?: string;
}

export function ActionButton({ onClick, loading, disabled, variant = 'primary', children, className = '' }: ActionButtonProps) {
  return (
    <button
      onClick={onClick}
      disabled={loading || disabled}
      className={`action-btn action-btn-${variant} disabled:opacity-40 disabled:cursor-not-allowed ${className}`}
    >
      {loading ? (
        <span className="flex items-center gap-1.5">
          <span className="w-3 h-3 border-2 border-current/30 border-t-current rounded-full animate-spin" />
          <span>...</span>
        </span>
      ) : children}
    </button>
  );
}
