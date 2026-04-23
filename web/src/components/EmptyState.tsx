/**
 * EmptyState — friendly placeholder for "nothing here yet" pages.
 *
 * Use anywhere a list / table / grid would otherwise render as a sad
 * blank or a one-liner. Shows an icon, a title, a helper line, and
 * optionally a primary CTA + secondary link. Keeps the empty
 * experience the same across Dashboard, Sessions, Live Grid, etc.
 */
import { ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { LucideIcon } from 'lucide-react';

interface Props {
  icon: LucideIcon;
  title: string;
  description?: ReactNode;
  primary?: {
    label: string;
    onClick?: () => void;
    to?: string;       // if set, renders as a Link instead of a button
    icon?: LucideIcon; // optional leading icon
  };
  secondary?: {
    label: string;
    href?: string;     // external link (target=_blank)
    to?: string;
  };
  /** Compact variant for embedding inside a section card */
  compact?: boolean;
}

export function EmptyState({ icon: Icon, title, description, primary, secondary, compact }: Props) {
  const padding = compact ? 'py-10 px-6' : 'py-20 px-8';
  return (
    <div className={`text-center ${padding} animate-fade-in`}>
      <div
        className="w-12 h-12 rounded-2xl flex items-center justify-center mx-auto mb-4"
        style={{ background: 'hsl(var(--primary) / 0.08)' }}
      >
        <Icon className="w-5 h-5 text-primary" />
      </div>
      <h3 className="text-base font-semibold text-foreground mb-1">{title}</h3>
      {description && (
        <p className="text-sm text-muted-foreground max-w-md mx-auto leading-relaxed">
          {description}
        </p>
      )}
      {(primary || secondary) && (
        <div className="flex items-center justify-center gap-3 mt-5">
          {primary &&
            (primary.to ? (
              <Link to={primary.to} className="action-btn action-btn-primary inline-flex items-center gap-1.5">
                {primary.icon && <primary.icon className="w-3.5 h-3.5" />}
                {primary.label}
              </Link>
            ) : (
              <button
                onClick={primary.onClick}
                className="action-btn action-btn-primary inline-flex items-center gap-1.5"
              >
                {primary.icon && <primary.icon className="w-3.5 h-3.5" />}
                {primary.label}
              </button>
            ))}
          {secondary &&
            (secondary.href ? (
              <a
                href={secondary.href}
                target="_blank"
                rel="noopener noreferrer"
                className="text-xs text-muted-foreground hover:text-foreground underline-offset-4 hover:underline"
              >
                {secondary.label}
              </a>
            ) : secondary.to ? (
              <Link
                to={secondary.to}
                className="text-xs text-muted-foreground hover:text-foreground underline-offset-4 hover:underline"
              >
                {secondary.label}
              </Link>
            ) : null)}
        </div>
      )}
    </div>
  );
}
