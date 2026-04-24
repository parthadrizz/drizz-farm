/**
 * Playback — video + event timeline for a completed (or live) session.
 *
 * Layout:
 *
 *   ┌─────────────────────┬──────────────────────┐
 *   │                     │                      │
 *   │  <video>            │  event list          │
 *   │  (session.mp4)      │  (clickable → seek)  │
 *   │                     │                      │
 *   ├─────────────────────┴──────────────────────┤
 *   │ ──●────────▼────●──────●──────────●────────│
 *   │ timeline with markers (SVG)                 │
 *   └─────────────────────────────────────────────┘
 *
 * Data: GET /api/v1/sessions/{id}/timeline returns
 *   { video_url, events: [{relative_s, type, level, message, …}] }
 *
 * The player uses relative_s for seek + highlight. Video playback
 * drives the "current event" cursor; clicks on events drive the
 * video's currentTime.
 *
 * Event types we render: lifecycle, logcat, network, screenshot.
 * Each gets a color + icon. Failures (level=error) float to top
 * visually via a left border.
 */
import { useEffect, useMemo, useRef, useState } from 'react';
import { useParams } from 'react-router-dom';
import {
  Play, Activity, Network, Image as ImageIcon, AlertCircle, Info, AlertTriangle,
} from 'lucide-react';

interface TimelineEvent {
  ts: string;
  relative_s: number;
  type: 'lifecycle' | 'logcat' | 'network' | 'screenshot';
  level?: 'info' | 'warn' | 'error';
  message: string;
  url?: string;
  status?: number;
  method?: string;
  duration_ms?: number;
}

interface TimelineResponse {
  session_id: string;
  started_at: string;
  video_url: string;
  artifact_dir: string;
  events: TimelineEvent[];
  total: number;
}

export function Playback() {
  const { id: sessionID } = useParams<{ id: string }>();
  const [data, setData] = useState<TimelineResponse | null>(null);
  const [error, setError] = useState('');
  const [filter, setFilter] = useState<Set<string>>(new Set(['lifecycle', 'logcat', 'network', 'screenshot']));
  const [levelFilter, setLevelFilter] = useState<Set<string>>(new Set(['info', 'warn', 'error']));
  const [now, setNow] = useState(0); // current video time in seconds
  const videoRef = useRef<HTMLVideoElement>(null);

  // Fetch timeline once on mount. We don't poll; the timeline is a
  // merge of artifacts that only finalize on release, so refreshing
  // mid-session brings partial data. User can refresh manually.
  useEffect(() => {
    if (!sessionID) return;
    let cancelled = false;
    fetch(`/api/v1/sessions/${sessionID}/timeline`)
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json();
      })
      .then((d) => { if (!cancelled) setData(d); })
      .catch((e) => { if (!cancelled) setError(e.message); });
    return () => { cancelled = true; };
  }, [sessionID]);

  // Hook the video's playback clock — updates once per 250ms when
  // playing, so the current-event highlight and timeline cursor stay
  // in sync without redrawing every frame.
  useEffect(() => {
    const v = videoRef.current;
    if (!v) return;
    let raf = 0;
    let last = 0;
    const tick = () => {
      const t = v.currentTime;
      if (Math.abs(t - last) > 0.25) {
        last = t;
        setNow(t);
      }
      raf = requestAnimationFrame(tick);
    };
    v.addEventListener('play', () => { raf = requestAnimationFrame(tick); });
    v.addEventListener('pause', () => cancelAnimationFrame(raf));
    v.addEventListener('seeked', () => setNow(v.currentTime));
    return () => cancelAnimationFrame(raf);
  }, [data?.video_url]);

  const events = useMemo(() => {
    if (!data) return [];
    return data.events.filter((e) => {
      if (!filter.has(e.type)) return false;
      const lvl = e.level || 'info';
      if (!levelFilter.has(lvl)) return false;
      return true;
    });
  }, [data, filter, levelFilter]);

  const duration = useMemo(() => {
    if (!events.length) return 60;
    // Max of event times + a bit of headroom for the cursor
    return Math.max(60, events[events.length - 1]?.relative_s + 10);
  }, [events]);

  const seekTo = (s: number) => {
    if (!videoRef.current) return;
    const t = Math.max(0, s);
    videoRef.current.currentTime = t;
    setNow(t);
    videoRef.current.play().catch(() => {});
  };

  if (error) {
    return (
      <div className="text-center py-20">
        <AlertCircle className="w-10 h-10 text-destructive mx-auto mb-3" />
        <div className="text-sm text-foreground">Couldn't load timeline</div>
        <div className="text-xs text-muted-foreground mt-1 font-mono">{error}</div>
      </div>
    );
  }
  if (!data) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="w-5 h-5 border-2 border-muted border-t-primary rounded-full animate-spin" />
        <span className="ml-3 text-muted-foreground text-sm">Loading timeline…</span>
      </div>
    );
  }

  return (
    <div className="space-y-4 animate-fade-in">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Session playback</h1>
          <p className="text-xs font-mono text-muted-foreground mt-0.5">
            {data.session_id} · started {new Date(data.started_at).toLocaleString()} · {data.total} events
          </p>
        </div>
        <a
          href={data.artifact_dir}
          target="_blank"
          rel="noopener noreferrer"
          className="action-btn surface-3 text-foreground text-xs"
        >
          Raw artifacts
        </a>
      </header>

      <div className="grid grid-cols-3 gap-4">
        {/* Video */}
        <div className="col-span-2">
          <div className="section-card overflow-hidden">
            <video
              ref={videoRef}
              controls
              preload="metadata"
              className="w-full aspect-video bg-black"
              src={data.video_url}
            />
          </div>
          <TimelineBar
            duration={duration}
            events={events}
            currentS={now}
            onSeek={seekTo}
          />
        </div>

        {/* Event list */}
        <div className="col-span-1">
          <FilterChips filter={filter} setFilter={setFilter} levelFilter={levelFilter} setLevelFilter={setLevelFilter} />
          <div className="section-card max-h-[640px] overflow-auto divide-y divide-border/50">
            {events.map((e, i) => {
              const nextTs = events[i + 1]?.relative_s ?? duration;
              const isCurrent = now >= e.relative_s && now < nextTs;
              return (
                <EventRow
                  key={`${e.relative_s}-${i}`}
                  event={e}
                  current={isCurrent}
                  sessionID={data.session_id}
                  onClick={() => seekTo(e.relative_s)}
                />
              );
            })}
            {events.length === 0 && (
              <div className="p-8 text-center text-xs text-muted-foreground">
                No events match the current filter.
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

/* ---- Timeline bar (SVG) -------------------------------------- */

function TimelineBar({
  duration, events, currentS, onSeek,
}: {
  duration: number; events: TimelineEvent[]; currentS: number; onSeek: (s: number) => void;
}) {
  const svgRef = useRef<SVGSVGElement>(null);
  const toX = (s: number) => Math.min(100, Math.max(0, (s / duration) * 100));

  const onClick = (e: React.MouseEvent<SVGSVGElement>) => {
    const r = svgRef.current?.getBoundingClientRect();
    if (!r) return;
    const frac = (e.clientX - r.left) / r.width;
    onSeek(frac * duration);
  };

  const markerFill = (ev: TimelineEvent) => {
    if (ev.level === 'error') return 'hsl(var(--destructive))';
    if (ev.level === 'warn') return 'hsl(45 95% 60%)';
    switch (ev.type) {
      case 'lifecycle': return 'hsl(var(--primary))';
      case 'network': return 'hsl(270 60% 70%)';
      case 'screenshot': return 'hsl(var(--accent))';
      case 'logcat': return 'hsl(var(--muted-foreground))';
    }
    return 'hsl(var(--foreground))';
  };

  return (
    <div className="mt-3 section-card p-3">
      <svg ref={svgRef} viewBox="0 0 100 20" className="w-full h-8 cursor-pointer" onClick={onClick} preserveAspectRatio="none">
        {/* baseline */}
        <line x1="0" y1="10" x2="100" y2="10" stroke="hsl(var(--border))" strokeWidth="0.2" />
        {/* event markers */}
        {events.map((e, i) => (
          <circle key={i} cx={toX(e.relative_s)} cy="10" r="0.9" fill={markerFill(e)}>
            <title>{formatTS(e.relative_s)} · {e.type} · {e.message}</title>
          </circle>
        ))}
        {/* cursor */}
        <line x1={toX(currentS)} y1="2" x2={toX(currentS)} y2="18" stroke="hsl(var(--primary))" strokeWidth="0.3" />
        <circle cx={toX(currentS)} cy="10" r="0.6" fill="hsl(var(--primary))" />
      </svg>
      <div className="flex items-center justify-between text-[10px] font-mono text-muted-foreground mt-1">
        <span>0:00</span>
        <span className="text-foreground">{formatTS(currentS)}</span>
        <span>{formatTS(duration)}</span>
      </div>
    </div>
  );
}

/* ---- Filter chips -------------------------------------------- */

function FilterChips({
  filter, setFilter, levelFilter, setLevelFilter,
}: {
  filter: Set<string>; setFilter: (s: Set<string>) => void;
  levelFilter: Set<string>; setLevelFilter: (s: Set<string>) => void;
}) {
  const toggle = (s: Set<string>, setter: (s: Set<string>) => void, key: string) => {
    const next = new Set(s);
    next.has(key) ? next.delete(key) : next.add(key);
    setter(next);
  };
  // Chips are click-to-filter toggles: active = this kind shows, inactive
  // = hidden. Used to render indistinguishable surface-2/3 tones that
  // made it impossible to tell which were selected. Now active chips
  // get a primary-colored border + filled background; inactive ones
  // are muted with a clear "hidden" look. A small checkmark reinforces
  // state for anyone who can't perceive the color difference.
  const chip = (active: boolean, label: string, onClick: () => void) => (
    <button
      onClick={onClick}
      aria-pressed={active}
      title={active ? `Hide ${label}` : `Show ${label}`}
      className={`px-2.5 py-1 rounded-md text-[11px] border transition inline-flex items-center gap-1 ${
        active
          ? 'border-primary bg-primary/15 text-foreground'
          : 'border-border bg-transparent text-muted-foreground/60 line-through opacity-60 hover:opacity-100 hover:text-foreground'
      }`}
    >
      {active && <span className="text-primary">✓</span>}
      {label}
    </button>
  );
  return (
    <div className="mb-3">
      <div className="text-[11px] text-muted-foreground mb-1.5">
        Click to toggle — active filters have a colored border; crossed-out ones are hidden.
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-[10px] uppercase tracking-wider text-muted-foreground/70 mr-1">Type</span>
        {chip(filter.has('lifecycle'), 'lifecycle', () => toggle(filter, setFilter, 'lifecycle'))}
        {chip(filter.has('logcat'), 'logcat', () => toggle(filter, setFilter, 'logcat'))}
        {chip(filter.has('network'), 'network', () => toggle(filter, setFilter, 'network'))}
        {chip(filter.has('screenshot'), 'screenshot', () => toggle(filter, setFilter, 'screenshot'))}
        <span className="mx-2 text-muted-foreground/30">│</span>
        <span className="text-[10px] uppercase tracking-wider text-muted-foreground/70 mr-1">Level</span>
        {chip(levelFilter.has('info'), 'info', () => toggle(levelFilter, setLevelFilter, 'info'))}
        {chip(levelFilter.has('warn'), 'warn', () => toggle(levelFilter, setLevelFilter, 'warn'))}
        {chip(levelFilter.has('error'), 'error', () => toggle(levelFilter, setLevelFilter, 'error'))}
      </div>
    </div>
  );
}

/* ---- Single event row ---------------------------------------- */

function EventRow({
  event, current, sessionID, onClick,
}: {
  event: TimelineEvent; current: boolean; sessionID: string; onClick: () => void;
}) {
  const Icon = iconFor(event);
  const levelColor =
    event.level === 'error' ? 'text-destructive'
    : event.level === 'warn' ? 'text-status-booting'
    : 'text-muted-foreground';

  return (
    <button
      onClick={onClick}
      className={`w-full text-left px-3 py-2 flex items-start gap-2 transition-colors hover:surface-2 ${
        current ? 'surface-2 border-l-2 border-primary' : ''
      } ${event.level === 'error' ? 'border-l-2 border-destructive/60' : ''}`}
    >
      <Icon className={`w-3.5 h-3.5 mt-0.5 flex-shrink-0 ${levelColor}`} />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-[10px] font-mono text-muted-foreground">{formatTS(event.relative_s)}</span>
          <span className="text-[10px] uppercase tracking-wider text-muted-foreground/70">{event.type}</span>
          {event.status && (
            <span className={`text-[10px] font-mono ${event.status >= 400 ? 'text-destructive' : 'text-muted-foreground'}`}>
              {event.method} {event.status}
            </span>
          )}
        </div>
        <div className="text-xs text-foreground truncate mt-0.5">{event.message}</div>
        {event.type === 'screenshot' && event.url && (
          <img
            loading="lazy"
            src={`/api/v1/sessions/${sessionID}/artifacts/${event.url}`}
            alt="screenshot"
            className="mt-1.5 max-w-[160px] rounded border border-border"
          />
        )}
      </div>
    </button>
  );
}

/* ---- Helpers ------------------------------------------------- */

function iconFor(e: TimelineEvent) {
  if (e.level === 'error') return AlertCircle;
  if (e.level === 'warn') return AlertTriangle;
  switch (e.type) {
    case 'lifecycle': return Play;
    case 'network': return Network;
    case 'screenshot': return ImageIcon;
    case 'logcat': return Activity;
  }
  return Info;
}

function formatTS(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  const m = Math.floor(s / 60);
  const rem = s % 60;
  return `${m}:${rem.toString().padStart(2, '0')}`;
}
