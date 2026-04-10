import { useEffect, useState } from 'react';
import { api, Session } from '../lib/api';

type Filter = 'all' | 'active' | 'queued' | 'released' | 'timed_out';

export function Sessions() {
  const [liveSessions, setLiveSessions] = useState<Session[]>([]);
  const [historySessions, setHistorySessions] = useState<any[]>([]);
  const [active, setActive] = useState(0);
  const [queued, setQueued] = useState(0);
  const [filter, setFilter] = useState<Filter>('all');
  const [error, setError] = useState('');

  const refresh = async () => {
    try {
      const [live, hist] = await Promise.all([
        api.listSessions(),
        fetch('/api/v1/history/sessions').then(r => r.json()).catch(() => []),
      ]);
      setLiveSessions(live.sessions || []);
      setActive(live.active);
      setQueued(live.queued);

      // History is from SQLite — has all past sessions
      const histList = Array.isArray(hist) ? hist : hist.sessions || [];
      setHistorySessions(histList);
      setError('');
    } catch (e: any) { setError(e.message); }
  };

  useEffect(() => { refresh(); const i = setInterval(refresh, 3000); return () => clearInterval(i); }, []);

  // Merge live + history, dedupe by ID, sort by created_at descending
  const allSessions = (() => {
    const map = new Map<string, any>();

    // History first (older)
    for (const s of historySessions) {
      map.set(s.id, { ...s, _source: 'history' });
    }
    // Live sessions override (fresher state)
    for (const s of liveSessions) {
      map.set(s.id, { ...s, _source: 'live' });
    }

    const list = Array.from(map.values());
    list.sort((a, b) => {
      // Active/queued first, then by created_at desc
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
    const text = s.connection?.adb_serial
      ? `adb connect ${s.connection.host}:${s.connection.adb_port}`
      : '';
    navigator.clipboard.writeText(text);
  };

  const tabStyle = (t: Filter) =>
    `px-3 py-1.5 text-xs font-medium rounded-md transition cursor-pointer ${
      filter === t
        ? 'bg-gray-700 text-white'
        : 'text-gray-500 hover:text-gray-300 hover:bg-gray-800'
    }`;

  const stateColor = (state: string) => {
    switch (state) {
      case 'active': return 'bg-emerald-400/10 text-emerald-400';
      case 'queued': return 'bg-yellow-400/10 text-yellow-400';
      case 'released': return 'bg-gray-700 text-gray-400';
      case 'timed_out': return 'bg-red-400/10 text-red-400';
      default: return 'bg-gray-700 text-gray-400';
    }
  };

  const dotColor = (state: string) => {
    switch (state) {
      case 'active': return 'bg-emerald-400';
      case 'queued': return 'bg-yellow-400 animate-pulse';
      case 'timed_out': return 'bg-red-400';
      default: return 'bg-gray-600';
    }
  };

  const formatDuration = (seconds: number) => {
    if (!seconds) return '-';
    if (seconds < 60) return `${seconds}s`;
    const m = Math.floor(seconds / 60);
    const s = seconds % 60;
    return `${m}m ${s}s`;
  };

  const formatTime = (ts: string) => {
    if (!ts) return '-';
    try { return new Date(ts).toLocaleTimeString(); } catch { return ts; }
  };

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Sessions</h1>
        <div className="flex items-center gap-4 text-sm">
          <span className="text-emerald-400">{active} active</span>
          <span className="text-yellow-400">{queued} queued</span>
          <span className="text-gray-500">{allSessions.length} total</span>
        </div>
      </div>

      {/* Filter tabs */}
      <div className="flex gap-1 border-b border-gray-800 pb-2">
        {(['all', 'active', 'queued', 'released', 'timed_out'] as Filter[]).map(t => (
          <button key={t} onClick={() => setFilter(t)} className={tabStyle(t)}>
            {t === 'timed_out' ? 'Timed Out' : t.charAt(0).toUpperCase() + t.slice(1)}
            <span className="ml-1.5 text-gray-600">{counts[t]}</span>
          </button>
        ))}
      </div>

      {error && <div className="text-red-400 text-sm">{error}</div>}

      {filtered.length === 0 ? (
        <div className="bg-gray-900 border border-gray-800 rounded-lg p-12 text-center text-gray-500">
          {filter === 'all' ? 'No sessions yet.' : `No ${filter} sessions.`}
        </div>
      ) : (
        <div className="bg-gray-900 border border-gray-800 rounded-lg divide-y divide-gray-800">
          {filtered.map(s => (
            <div key={s.id} className="p-4">
              <div className="flex items-center gap-4">
                <div className={`w-2.5 h-2.5 rounded-full flex-shrink-0 ${dotColor(s.state)}`} />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-3">
                    <span className="text-sm font-mono text-gray-200">{s.id}</span>
                    {s.client_name && (
                      <span className="text-xs text-purple-400 bg-purple-400/10 px-1.5 py-0.5 rounded">{s.client_name}</span>
                    )}
                  </div>
                  <div className="text-xs text-gray-500 mt-0.5 flex items-center gap-3">
                    <span>{s.profile || 'auto'} · {s.platform}</span>
                    <span>{formatTime(s.created_at)}</span>
                    {(s.duration_seconds > 0) && (
                      <span className="text-gray-600">duration: {formatDuration(s.duration_seconds)}</span>
                    )}
                    {s.serial && <span className="font-mono text-gray-600">{s.serial}</span>}
                    {s.connection?.adb_serial && <span className="font-mono text-gray-600">{s.connection.adb_serial}</span>}
                  </div>
                </div>
                <span className={`text-xs px-2 py-0.5 rounded font-medium uppercase ${stateColor(s.state)}`}>
                  {s.state === 'timed_out' ? 'TIMED OUT' : s.state}
                </span>
              </div>

              {/* Active session details */}
              {s.state === 'active' && s.connection && (
                <div className="mt-3 ml-6 bg-gray-800/50 rounded p-3">
                  <div className="flex items-center justify-between">
                    <div className="text-xs text-gray-400 space-y-1">
                      <div>Serial: <span className="text-gray-200 font-mono">{s.connection.adb_serial}</span></div>
                      <div>ADB: <span className="text-gray-200 font-mono">{s.connection.host}:{s.connection.adb_port}</span></div>
                      {s.connection.console_port && (
                        <div>Console: <span className="text-gray-200 font-mono">{s.connection.host}:{s.connection.console_port}</span></div>
                      )}
                      {s.connection.appium_url && (
                        <div>Appium: <span className="text-gray-200 font-mono">{s.connection.appium_url}</span></div>
                      )}
                      <div>Expires: <span className="text-gray-200">{formatTime(s.expires_at)}</span></div>
                    </div>
                    <div className="flex flex-col gap-2">
                      <button onClick={() => copyConnection(s)}
                        className="px-3 py-1.5 bg-gray-700 text-gray-300 rounded text-xs hover:bg-gray-600 transition">
                        Copy ADB
                      </button>
                      <button onClick={() => { api.releaseSession(s.id).then(refresh); }}
                        className="px-3 py-1.5 bg-red-500/10 text-red-400 rounded text-xs font-medium hover:bg-red-500/20 transition">
                        Release
                      </button>
                    </div>
                  </div>
                </div>
              )}

              {/* Artifacts for completed sessions */}
              {(s.state === 'released' || s.state === 'timed_out') && (
                <div className="mt-2 ml-6 flex items-center gap-3">
                  <button onClick={async () => {
                    const instId = s.instance_id || s.id;
                    const res = await fetch(`/api/v1/sessions/${s.id}/recordings`).catch(() => null);
                    if (res?.ok) {
                      const data = await res.json();
                      const recs = data.recordings || [];
                      if (recs.length > 0) {
                        window.open(`/api/v1/sessions/${s.id}/recording/download?file=${recs[0]}`, '_blank');
                      } else {
                        alert('No recordings found for this session');
                      }
                    }
                  }} className="text-xs text-blue-400 hover:text-blue-300 flex items-center gap-1">
                    <span>Recording</span>
                  </button>
                  <button onClick={() => {
                    window.open(`/api/v1/sessions/${s.id}/logcat/download`, '_blank');
                  }} className="text-xs text-blue-400 hover:text-blue-300 flex items-center gap-1">
                    <span>Logcat</span>
                  </button>
                  <button onClick={async () => {
                    const res = await fetch(`/api/v1/sessions/${s.id}/screenshot`, { method: 'POST' });
                    if (res.ok) {
                      const data = await res.json();
                      alert(`Screenshot: ${data.path || 'saved'}`);
                    }
                  }} className="text-xs text-blue-400 hover:text-blue-300 flex items-center gap-1">
                    <span>Screenshot</span>
                  </button>
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
