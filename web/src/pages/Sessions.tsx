import { useEffect, useState } from 'react';
import { api, Session } from '../lib/api';

export function Sessions() {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [active, setActive] = useState(0);
  const [queued, setQueued] = useState(0);
  const [error, setError] = useState('');

  const refresh = async () => {
    try {
      const r = await api.listSessions();
      setSessions(r.sessions || []); setActive(r.active); setQueued(r.queued); setError('');
    } catch (e: any) { setError(e.message); }
  };

  useEffect(() => { refresh(); const i = setInterval(refresh, 3000); return () => clearInterval(i); }, []);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Sessions</h1>
        <div className="flex gap-4 text-sm">
          <span className="text-emerald-400">{active} active</span>
          <span className="text-yellow-400">{queued} queued</span>
        </div>
      </div>
      {error && <div className="text-red-400 text-sm">{error}</div>}
      {sessions.length === 0 ? (
        <div className="bg-gray-900 border border-gray-800 rounded-lg p-12 text-center text-gray-500">No sessions</div>
      ) : (
        <div className="bg-gray-900 border border-gray-800 rounded-lg divide-y divide-gray-800">
          {sessions.map(s => (
            <div key={s.id} className="p-4 flex items-center gap-4">
              <div className={`w-2.5 h-2.5 rounded-full ${s.state === 'active' ? 'bg-emerald-400' : s.state === 'queued' ? 'bg-yellow-400 animate-pulse' : 'bg-gray-600'}`} />
              <div className="flex-1 min-w-0">
                <div className="text-sm font-mono text-gray-200">{s.id}</div>
                <div className="text-xs text-gray-500 mt-0.5">{s.connection.adb_serial} · {s.profile}</div>
              </div>
              <div className="text-right text-xs text-gray-500">
                <div>{new Date(s.created_at).toLocaleTimeString()}</div>
                <div>expires {new Date(s.expires_at).toLocaleTimeString()}</div>
              </div>
              {s.state === 'active' && (
                <button onClick={() => { api.releaseSession(s.id).then(refresh); }}
                  className="px-3 py-1 bg-red-500/10 text-red-400 rounded text-xs font-medium hover:bg-red-500/20 transition">Release</button>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
