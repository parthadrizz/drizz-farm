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

  const copyConnection = (s: Session) => {
    const text = s.connection.adb_serial
      ? `adb connect ${s.connection.host}:${s.connection.adb_port}`
      : `UDID: ${s.connection.host}`;
    navigator.clipboard.writeText(text);
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Sessions</h1>
        <div className="flex items-center gap-4">
          <span className="text-sm text-emerald-400">{active} active</span>
          <span className="text-sm text-yellow-400">{queued} queued</span>
        </div>
      </div>

      {error && <div className="text-red-400 text-sm">{error}</div>}

      {sessions.length === 0 ? (
        <div className="bg-gray-900 border border-gray-800 rounded-lg p-12 text-center text-gray-500">
          No sessions. Click "New Session" or use the CLI.
        </div>
      ) : (
        <div className="bg-gray-900 border border-gray-800 rounded-lg divide-y divide-gray-800">
          {sessions.map(s => (
            <div key={s.id} className="p-4">
              <div className="flex items-center gap-4">
                <div className={`w-2.5 h-2.5 rounded-full flex-shrink-0 ${
                  s.state === 'active' ? 'bg-emerald-400' :
                  s.state === 'queued' ? 'bg-yellow-400 animate-pulse' :
                  'bg-gray-600'
                }`} />
                <div className="flex-1 min-w-0">
                  <div className="text-sm font-mono text-gray-200">{s.id}</div>
                  <div className="text-xs text-gray-500 mt-0.5">{s.profile || 'auto'} · {s.platform}</div>
                </div>
                <span className={`text-xs px-2 py-0.5 rounded font-medium uppercase ${
                  s.state === 'active' ? 'bg-emerald-400/10 text-emerald-400' :
                  s.state === 'queued' ? 'bg-yellow-400/10 text-yellow-400' :
                  s.state === 'released' ? 'bg-gray-700 text-gray-400' :
                  'bg-gray-700 text-gray-400'
                }`}>{s.state}</span>
              </div>

              {/* Connection details */}
              {s.state === 'active' && s.connection && (
                <div className="mt-3 ml-6.5 bg-gray-800/50 rounded p-3">
                  <div className="flex items-center justify-between">
                    <div className="text-xs text-gray-400 space-y-1">
                      <div>Serial: <span className="text-gray-200 font-mono">{s.connection.adb_serial}</span></div>
                      <div>ADB: <span className="text-gray-200 font-mono">{s.connection.host}:{s.connection.adb_port}</span></div>
                      {s.connection.console_port && (
                        <div>Console: <span className="text-gray-200 font-mono">{s.connection.host}:{s.connection.console_port}</span></div>
                      )}
                      <div>Created: <span className="text-gray-200">{new Date(s.created_at).toLocaleTimeString()}</span></div>
                      <div>Expires: <span className="text-gray-200">{new Date(s.expires_at).toLocaleTimeString()}</span></div>
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
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
