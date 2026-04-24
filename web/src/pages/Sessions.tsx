import { useEffect, useState } from 'react';
import { Activity, Plus } from 'lucide-react';
import { api, Session } from '../lib/api';
import { StatusBadge, StatusDot } from '../components/StatusBadge';
import { EmptyState } from '../components/EmptyState';
import { NewSessionModal } from '../components/NewSessionModal';

type Filter = 'all' | 'active' | 'queued' | 'released' | 'timed_out';

export function Sessions() {
  const [liveSessions, setLiveSessions] = useState<Session[]>([]);
  const [historySessions, setHistorySessions] = useState<any[]>([]);
  const [active, setActive] = useState(0);
  const [queued, setQueued] = useState(0);
  const [filter, setFilter] = useState<Filter>('all');
  const [error, setError] = useState('');
  const [newOpen, setNewOpen] = useState(false);

  const refresh = async () => {
    try {
      const [live, hist] = await Promise.all([
        api.listSessions(),
        fetch('/api/v1/history/sessions').then(r => r.json()).catch(() => []),
      ]);
      setLiveSessions(live.sessions || []);
      setActive(live.active); setQueued(live.queued);
      const histList = Array.isArray(hist) ? hist : hist.sessions || [];
      setHistorySessions(histList);
      setError('');
    } catch (e: any) { setError(e.message); }
  };

  useEffect(() => { refresh(); const i = setInterval(refresh, 3000); return () => clearInterval(i); }, []);

  const allSessions = (() => {
    const map = new Map<string, any>();
    for (const s of historySessions) map.set(s.id, { ...s, _source: 'history' });
    for (const s of liveSessions) map.set(s.id, { ...s, _source: 'live' });
    const list = Array.from(map.values());
    list.sort((a, b) => {
      const stateOrder: Record<string, number> = { queued: 0, active: 1, released: 2, timed_out: 3 };
      const sa = stateOrder[a.state] ?? 2;
      const sb = stateOrder[b.state] ?? 2;
      if (sa !== sb) return sa - sb;
      return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
    });
    return list;
  })();

  const filtered = filter === 'all' ? allSessions : allSessions.filter(s => s.state === filter);
  const counts: Record<Filter, number> = {
    all: allSessions.length,
    active: allSessions.filter(s => s.state === 'active').length,
    queued: allSessions.filter(s => s.state === 'queued').length,
    released: allSessions.filter(s => s.state === 'released').length,
    timed_out: allSessions.filter(s => s.state === 'timed_out').length,
  };

  const copyConnection = (s: Session) => {
    const text = s.connection?.adb_serial ? `adb connect ${s.connection.host}:${s.connection.adb_port}` : '';
    navigator.clipboard.writeText(text);
  };

  const formatDuration = (seconds: number) => {
    if (!seconds) return '-';
    if (seconds < 60) return `${seconds}s`;
    const m = Math.floor(seconds / 60);
    const s = seconds % 60;
    return `${m}m ${s}s`;
  };

  // AVD names are typically snake_case like "Pixel_7_API_34" or
  // "drizz_api34_ext8_play_0". Snake_case reads as a filename; swap
  // underscores for spaces so it reads as a device name in the UI.
  // Leave the underlying name intact everywhere else (Appium, adb,
  // avdmanager all want the exact form).
  const prettyDeviceName = (name: string) => name.replace(/_/g, ' ');

  // Sessions list spans multiple days once you've been running for a
  // while — showing only the clock time ("2:55:44 PM") made it
  // impossible to tell yesterday's run from today's. Render as "Apr 24,
  // 14:55" — locale-aware date + 24h time — so both are unambiguous.
  const formatTime = (ts: string) => {
    if (!ts) return '-';
    try {
      const d = new Date(ts);
      const date = d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
      const time = d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hour12: false });
      return `${date}, ${time}`;
    } catch { return ts; }
  };

  return (
    <div className="space-y-5 animate-fade-in">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-bold text-foreground">Sessions</h1>
        <div className="flex items-center gap-4 text-sm font-mono">
          <span className="text-primary">{active} active</span>
          <span className="text-status-booting">{queued} queued</span>
          <span className="text-muted-foreground">{allSessions.length} total</span>
          <button
            onClick={() => setNewOpen(true)}
            className="action-btn action-btn-primary inline-flex items-center gap-1.5 ml-2"
          >
            <Plus className="w-3.5 h-3.5" /> New session
          </button>
        </div>
      </div>

      <NewSessionModal
        open={newOpen}
        onClose={() => setNewOpen(false)}
        onCreated={() => { setNewOpen(false); refresh(); }}
      />

      <div className="flex gap-1 border-b border-border pb-2">
        {(['all', 'active', 'queued', 'released', 'timed_out'] as Filter[]).map(t => (
          <button key={t} onClick={() => setFilter(t)}
            className={`px-3 py-1.5 text-xs font-medium rounded-md transition ${
              filter === t ? 'surface-3 text-foreground' : 'text-muted-foreground hover:text-foreground hover:surface-2'
            }`}>
            {t === 'timed_out' ? 'Timed Out' : t.charAt(0).toUpperCase() + t.slice(1)}
            <span className="ml-1.5 opacity-50">{counts[t]}</span>
          </button>
        ))}
      </div>

      {error && <div className="text-destructive text-sm">{error}</div>}

      {filtered.length === 0 ? (
        <div className="section-card">
          <EmptyState
            icon={Activity}
            title={filter === 'all' ? 'No sessions yet' : `No ${filter} sessions`}
            description={
              filter === 'all'
                ? 'Sessions are how you reserve a device for a test run. Start one from your test framework, the MCP server, or POST /api/v1/sessions.'
                : 'Switch the filter above to see other states.'
            }
            secondary={filter === 'all' ? { label: 'Sessions API docs', href: 'https://github.com/parthadrizz/drizz-farm#cli-reference' } : undefined}
          />
        </div>
      ) : (
        <div className="section-card divide-y divide-border/50">
          {filtered.map(s => (
            <div key={s.id} className="p-4 card-hover">
              <div className="flex items-center gap-4">
                <StatusDot state={s.state} />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-3">
                    <span className="text-sm font-mono text-foreground">{s.id}</span>
                    {s.client_name && <span className="badge" style={{ background: 'hsl(270 60% 70% / 0.1)', color: 'hsl(270, 60%, 70%)', border: '1px solid hsl(270 60% 70% / 0.2)' }}>{s.client_name}</span>}
                  </div>
                  <div className="text-xs text-muted-foreground mt-1 flex items-center gap-3 flex-wrap">
                    {(s.node_name || s.connection?.node_name) && (
                      <span className="badge" style={{ background: 'hsl(var(--accent) / 0.1)', color: 'hsl(var(--accent))', border: '1px solid hsl(var(--accent) / 0.2)' }}>{s.node_name || s.connection?.node_name}</span>
                    )}
                    {/* Device name (e.g. "Pixel 7 · API 34") is the
                        primary identifier — it's what the user picked
                        in the New Session modal and what they'd recognise.
                        Profile and platform demoted to secondary info,
                        the opaque emulator-5556 serial hidden in the
                        expanded detail panel. */}
                    {s.device_name && (
                      <span className="text-foreground font-medium">{prettyDeviceName(s.device_name)}</span>
                    )}
                    <span className="opacity-60">{s.profile || 'auto'} · {s.platform}</span>
                    <span>{formatTime(s.created_at)}</span>
                    {(s.duration_seconds && s.duration_seconds > 0) && <span className="opacity-60">duration: {formatDuration(s.duration_seconds)}</span>}
                  </div>
                </div>
                <StatusBadge state={s.state} />
              </div>

              {s.state === 'active' && s.connection && (
                <div className="mt-3 ml-7 surface-2 rounded-lg p-4">
                  <div className="flex items-center justify-between">
                    <div className="text-xs text-muted-foreground space-y-1.5">
                      <div>Serial: <span className="text-foreground font-mono">{s.connection.adb_serial}</span></div>
                      <div>ADB: <span className="text-foreground font-mono">{s.connection.host}:{s.connection.adb_port}</span></div>
                      {s.connection.console_port && <div>Console: <span className="text-foreground font-mono">{s.connection.host}:{s.connection.console_port}</span></div>}
                      {s.connection.appium_url && <div>Appium: <span className="text-foreground font-mono">{s.connection.appium_url}</span></div>}
                      <div>Expires: <span className="text-foreground">{formatTime(s.expires_at)}</span></div>
                    </div>
                    <div className="flex flex-col gap-2">
                      <button onClick={() => copyConnection(s)} className="action-btn surface-3 text-foreground hover:surface-3">Copy ADB</button>
                      <button onClick={() => { api.releaseSession(s.id).then(refresh); }} className="action-btn action-btn-danger">Release</button>
                    </div>
                  </div>
                </div>
              )}

              <ArtifactsPanel sessionID={s.id} autoExpand={s.state === 'active'} />

              {/* Playback link — only meaningful once the session
                  has finished, because the video.mp4 finalizes on
                  release. For active sessions we leave a hint but
                  don't link (playback against an in-flight video
                  is flaky and confuses users). */}
              {(s.state === 'released' || s.state === 'timed_out' || s.state === 'interrupted') && (
                <div className="mt-2 ml-7">
                  <a
                    href={`/playback/${s.id}`}
                    className="text-xs text-primary hover:underline inline-flex items-center gap-1"
                  >
                    ▶ Open session playback
                  </a>
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// ArtifactsPanel lazy-loads the unified /sessions/{id}/artifacts list
// on demand. Previously this fetched on mount for every row, so a
// page with 160 sessions in history hammered the daemon with 160
// artifact requests per render — one slow machine noticeably slowed
// the UI. Now a single "Artifacts" button fires one fetch when the
// user expands the row. Active sessions auto-expand so you can see
// in-progress captures land.
function ArtifactsPanel({ sessionID, autoExpand }: { sessionID: string; autoExpand?: boolean }) {
  const [data, setData] = useState<{ capabilities?: any; artifacts: ArtifactRow[] } | null>(null);
  const [expanded, setExpanded] = useState<boolean>(!!autoExpand);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!expanded) return;
    let cancelled = false;
    const load = () => {
      setLoading(true);
      fetch(`/api/v1/sessions/${sessionID}/artifacts`)
        .then(r => r.ok ? r.json() : null)
        .then(d => { if (!cancelled) { setData(d); setLoading(false); } })
        .catch(() => { if (!cancelled) setLoading(false); });
    };
    load();
    // Only poll for active (auto-expanded) sessions — released ones
    // don't need revalidation every minute.
    if (autoExpand) {
      const i = setInterval(load, 60_000);
      return () => { cancelled = true; clearInterval(i); };
    }
    return () => { cancelled = true; };
  }, [sessionID, expanded, autoExpand]);

  if (!expanded) {
    return (
      <button
        onClick={() => setExpanded(true)}
        className="mt-2 ml-7 text-[11px] text-muted-foreground hover:text-primary"
      >
        ▸ Artifacts
      </button>
    );
  }

  if (loading && !data) {
    return <div className="mt-2 ml-7 text-[11px] text-muted-foreground">Loading artifacts…</div>;
  }
  if (!data) return null;

  const arts = data.artifacts || [];
  if (arts.length === 0) {
    const caps = data.capabilities;
    const asked = caps && (caps.record_video || caps.capture_logcat || caps.capture_screenshots || caps.capture_network);
    if (!asked) {
      return <div className="mt-2 ml-7 text-[11px] text-muted-foreground">No captures requested for this session.</div>;
    }
    return <div className="mt-2 ml-7 text-[11px] text-muted-foreground">Artifacts requested but not yet available.</div>;
  }
  return (
    <div className="mt-2 ml-7 flex items-center gap-2 flex-wrap">
      {arts.map(a => (
        <a
          key={a.filename}
          href={a.url}
          target="_blank"
          rel="noopener noreferrer"
          className="action-btn surface-3 text-xs text-foreground hover:text-primary"
          title={`${a.filename} · ${formatSize(a.size)}`}
        >
          {iconForType(a.type)}
          <span className="ml-1.5">{a.type}</span>
          <span className="ml-1 text-muted-foreground/70">· {formatSize(a.size)}</span>
        </a>
      ))}
    </div>
  );
}

interface ArtifactRow {
  type: string;
  filename: string;
  size: number;
  url: string;
}

function iconForType(t: string): string {
  switch (t) {
    case 'video': return '▶';
    case 'logcat': return '▤';
    case 'screenshot': return '◉';
    case 'network': return '⇅';
    default: return '•';
  }
}

function formatSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`;
}
