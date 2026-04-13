interface StatusBadgeProps {
  state: string;
  className?: string;
}

const stateClasses: Record<string, string> = {
  warm: 'badge-warm',
  allocated: 'badge-allocated',
  booting: 'badge-booting',
  resetting: 'badge-booting',
  error: 'badge-error',
  offline: 'badge-offline',
  active: 'badge-warm',
  queued: 'badge-booting',
  released: 'badge-offline',
  timed_out: 'badge-error',
};

export function StatusBadge({ state, className = '' }: StatusBadgeProps) {
  const label = state === 'timed_out' ? 'TIMED OUT' : state;
  return (
    <span className={`badge ${stateClasses[state] || 'badge-offline'} ${className}`}>
      {label}
    </span>
  );
}

export function StatusDot({ state, className = '' }: StatusBadgeProps) {
  const dotClasses: Record<string, string> = {
    warm: 'status-dot-warm',
    running: 'status-dot-running',
    allocated: 'status-dot-allocated',
    booting: 'status-dot-booting animate-pulse',
    resetting: 'status-dot-booting animate-pulse',
    error: 'status-dot-error',
    offline: 'status-dot-offline',
    active: 'status-dot-running',
    queued: 'status-dot-booting animate-pulse',
  };
  return <span className={`status-dot ${dotClasses[state] || 'status-dot-offline'} ${className}`} />;
}
