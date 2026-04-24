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

// Tab keys for the event pane. 'all' merges every type; the named
// tabs show just that source. Simpler than the previous grid of
// toggle-chips — one click per view, always obvious what's selected.
type TabKey = 'all' | 'lifecycle' | 'logcat' | 'network' | 'screenshot';

export function Playback() {
  const { id: sessionID } = useParams<{ id: string }>();
  const [data, setData] = useState<TimelineResponse | null>(null);
  const [error, setError] = useState('');
  const [tab, setTab] = useState<TabKey>('all');
  // Level filter stays as a chip row — orthogonal to the tab (you
  // might want "only errors across all types" or "only errors in logcat").
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

  // Counts per tab — rendered as badges on each tab so the user can
  // see at a glance whether a stream has anything in it before
  // clicking. Zero counts are common (no network HAR, no screenshots).
  const counts = useMemo(() => {
    const c = { all: 0, lifecycle: 0, logcat: 0, network: 0, screenshot: 0 } as Record<TabKey, number>;
    if (!data) return c;
    for (const e of data.events) {
      c.all++;
      if (e.type in c) c[e.type as TabKey]++;
    }
    return c;
  }, [data]);

  const events = useMemo(() => {
    if (!data) return [];
    return data.events.filter((e) => {
      if (tab !== 'all' && e.type !== tab) return false;
      const lvl = e.level || 'info';
      if (!levelFilter.has(lvl)) return false;
      return true;
    });
  }, [data, tab, levelFilter]);

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
          <Tabs tab={tab} setTab={setTab} counts={counts} />
          <LevelChips levelFilter={levelFilter} setLevelFilter={setLevelFilter} />
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

/* ---- Tabs (event type) --------------------------------------- */

// One tab per event source + an "All" tab that merges everything.
// Each tab shows its count so zero-event streams are obvious without
// clicking. The active tab gets a filled primary background so you
// can always tell which view you're on.
function Tabs({
  tab, setTab, counts,
}: {
  tab: TabKey; setTab: (t: TabKey) => void; counts: Record<TabKey, number>;
}) {
  const items: { key: TabKey; label: string }[] = [
    { key: 'all',        label: 'All' },
    { key: 'lifecycle',  label: 'Lifecycle' },
    { key: 'logcat',     label: 'Logcat' },
    { key: 'network',    label: 'Network' },
    { key: 'screenshot', label: 'Screenshots' },
  ];
  return (
    <div className="flex items-center gap-1 mb-2 border-b border-border overflow-x-auto">
      {items.map(({ key, label }) => {
        const active = tab === key;
        const n = counts[key];
        return (
          <button
            key={key}
            onClick={() => setTab(key)}
            aria-pressed={active}
            className={`px-3 py-1.5 text-xs whitespace-nowrap border-b-2 -mb-px transition inline-flex items-center gap-1.5 ${
              active
                ? 'border-primary text-foreground font-medium'
                : 'border-transparent text-muted-foreground hover:text-foreground'
            }`}
          >
            {label}
            <span className={`text-[10px] px-1.5 py-0.5 rounded-full ${
              active ? 'bg-primary/20 text-primary' : 'bg-surface-2 text-muted-foreground/70'
            }`}>{n}</span>
          </button>
        );
      })}
    </div>
  );
}

/* ---- Level chips (orthogonal to tabs) ------------------------ */

function LevelChips({
  levelFilter, setLevelFilter,
}: {
  levelFilter: Set<string>; setLevelFilter: (s: Set<string>) => void;
}) {
  const toggle = (key: string) => {
    const next = new Set(levelFilter);
    next.has(key) ? next.delete(key) : next.add(key);
    setLevelFilter(next);
  };
  const chip = (key: 'info' | 'warn' | 'error') => {
    const active = levelFilter.has(key);
    const color = key === 'error' ? 'hsl(var(--destructive))'
                : key === 'warn'  ? '#d97706'
                : 'hsl(var(--muted-foreground))';
    return (
      <button
        onClick={() => toggle(key)}
        aria-pressed={active}
        title={active ? `Hide ${key}` : `Show ${key}`}
        className={`px-2 py-0.5 rounded-md text-[11px] border transition inline-flex items-center gap-1 ${
          active ? 'border-border bg-surface-2 text-foreground' : 'border-border/50 text-muted-foreground/50 line-through'
        }`}
      >
        <span className="w-1.5 h-1.5 rounded-full" style={{ background: active ? color : 'currentColor', opacity: active ? 1 : 0.4 }} />
        {key}
      </button>
    );
  };
  return (
    <div className="flex items-center gap-1.5 mb-2 pl-1 text-[11px]">
      <span className="text-muted-foreground/70 mr-1">Level:</span>
      {chip('info')}
      {chip('warn')}
      {chip('error')}
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
