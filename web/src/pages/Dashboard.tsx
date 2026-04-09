import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api, PoolStatus, NodeHealth, DeviceInstance } from '../lib/api';

export function Dashboard() {
  const [pool, setPool] = useState<PoolStatus | null>(null);
  const [health, setHealth] = useState<NodeHealth | null>(null);
  const [avds, setAvds] = useState<string[]>([]);
  const [error, setError] = useState('');
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const navigate = useNavigate();

  const refresh = async () => {
    try {
      const [p, h, a] = await Promise.all([api.pool(), api.health(), api.avds()]);
      setPool(p); setHealth(h); setAvds((a.avds || []).map(x => x.name)); setError('');
    } catch (e: any) { setError(e.message); }
  };

  useEffect(() => { refresh(); const i = setInterval(refresh, 3000); return () => clearInterval(i); }, []);

  const handleBoot = async (avdName: string) => {
    setActionLoading(avdName);
    try { await api.bootAVD(avdName); refresh(); } catch (e: any) { alert(e.message); }
    setActionLoading(null);
  };

  const handleShutdown = async (inst: DeviceInstance) => {
    setActionLoading(inst.id);
    try { await api.shutdownInstance(inst.id); refresh(); } catch (e: any) { alert(e.message); }
    setActionLoading(null);
  };

  const handleRelease = async (sessionId: string) => {
    try { await api.releaseSession(sessionId); refresh(); } catch (e: any) { alert(e.message); }
  };

  if (error) return (
    <div className="text-center py-20">
      <div className="text-red-400 text-lg mb-2">Cannot connect to daemon</div>
      <div className="text-gray-500 text-sm">{error}</div>
      <div className="text-gray-600 text-sm mt-4">Run: <code className="bg-gray-800 px-2 py-1 rounded">drizz-farm start</code></div>
    </div>
  );
  if (!pool || !health) return <div className="text-center py-20 text-gray-500">Connecting...</div>;

  // Build lookup: pool instances by device name
  const poolByName = new Map<string, DeviceInstance>();
  pool.instances.forEach(inst => poolByName.set(inst.device_name, inst));

  const stats = [
    { label: 'Capacity', value: pool.total_capacity },
    { label: 'Warm', value: pool.warm, color: 'text-emerald-400' },
    { label: 'Allocated', value: pool.allocated, color: 'text-blue-400' },
    { label: 'Booting', value: pool.booting, color: 'text-yellow-400' },
    { label: 'Error', value: pool.error, color: 'text-red-400' },
  ];

  return (
    <div className="space-y-8">
      {/* Stats */}
      <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
        {stats.map(s => (
          <div key={s.label} className="bg-gray-900 rounded-lg border border-gray-800 p-4">
            <div className="text-xs text-gray-500 uppercase tracking-wider">{s.label}</div>
            <div className={`text-2xl font-bold mt-1 ${s.color || 'text-gray-200'}`}>{s.value}</div>
          </div>
        ))}
      </div>

      {/* Node */}
      <div className="bg-gray-900 rounded-lg border border-gray-800 p-4">
        <div className="flex items-center justify-between mb-3">
          <h2 className="text-sm font-semibold text-gray-400 uppercase tracking-wider">Node</h2>
          <span className="text-xs text-emerald-400 bg-emerald-400/10 px-2 py-0.5 rounded">{health.status}</span>
        </div>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
          <div><span className="text-gray-500">Name:</span> <span className="text-gray-200">{health.node}</span></div>
          <div><span className="text-gray-500">Version:</span> <span className="text-gray-200">{health.version}</span></div>
          <div><span className="text-gray-500">Uptime:</span> <span className="text-gray-200">{health.uptime}</span></div>
          <div><span className="text-gray-500">CPUs:</span> <span className="text-gray-200">{health.resources.num_cpu}</span></div>
        </div>
      </div>

      {/* Devices — shows all AVDs + USB devices with controls */}
      <div className="bg-gray-900 rounded-lg border border-gray-800">
        <div className="px-4 py-3 border-b border-gray-800 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-gray-400 uppercase tracking-wider">Devices</h2>
          <span className="text-xs text-gray-500">{avds.length} AVDs · {pool.instances.filter(i => i.device_kind === 'android_usb').length} USB</span>
        </div>

        <div className="divide-y divide-gray-800">
          {/* AVDs */}
          {avds.map(avdName => {
            const inst = poolByName.get(avdName);
            const state = inst?.state || 'offline';
            const isLoading = actionLoading === avdName || actionLoading === inst?.id;

            return (
              <div key={avdName} className="px-4 py-3 flex items-center gap-4">
                <StateIcon state={state} />
                <div className="flex-1 min-w-0">
                  <div className="text-sm font-medium text-gray-200 truncate">{avdName}</div>
                  <div className="text-xs text-gray-500">
                    {inst ? `${inst.serial} · port:${inst.connection.adb_port || '-'}` : 'emulator · offline'}
                  </div>
                </div>

                {/* State badge */}
                <StateBadge state={state} />

                {/* Actions */}
                <div className="flex gap-1.5">
                  {state === 'offline' && (
                    <ActionBtn onClick={() => handleBoot(avdName)} loading={isLoading} color="emerald">Start</ActionBtn>
                  )}
                  {(state === 'warm' || state === 'allocated') && inst && (
                    <ActionBtn onClick={() => navigate(`/live/${inst.id}`)} color="blue">View</ActionBtn>
                  )}
                  {state === 'warm' && (
                    <ActionBtn onClick={() => inst && handleShutdown(inst)} loading={isLoading} color="red">Stop</ActionBtn>
                  )}
                  {state === 'allocated' && inst?.session_id && (
                    <ActionBtn onClick={() => handleRelease(inst.session_id!)} color="orange">Release</ActionBtn>
                  )}
                  {state === 'booting' && (
                    <span className="text-xs text-yellow-400 animate-pulse">Booting...</span>
                  )}
                  {state === 'resetting' && (
                    <span className="text-xs text-orange-400 animate-pulse">Resetting...</span>
                  )}
                </div>
              </div>
            );
          })}

          {/* USB devices (not in AVD list) */}
          {pool.instances
            .filter(inst => inst.device_kind === 'android_usb')
            .map(inst => (
              <div key={inst.id} className="px-4 py-3 flex items-center gap-4">
                <StateIcon state={inst.state} />
                <div className="flex-1 min-w-0">
                  <div className="text-sm font-medium text-gray-200 truncate">{inst.device_name}</div>
                  <div className="text-xs text-gray-500">USB · {inst.serial}</div>
                </div>
                <StateBadge state={inst.state} />
                <div className="flex gap-1.5">
                  {inst.state === 'allocated' && inst.session_id && (
                    <ActionBtn onClick={() => handleRelease(inst.session_id!)} color="orange">Release</ActionBtn>
                  )}
                </div>
              </div>
            ))}

          {avds.length === 0 && pool.instances.length === 0 && (
            <div className="p-8 text-center text-gray-500">No devices. Go to Create to add AVDs.</div>
          )}
        </div>
      </div>
    </div>
  );
}

function ActionBtn({ onClick, loading, color, children }: { onClick: () => void; loading?: boolean; color: string; children: React.ReactNode }) {
  const colors: Record<string, string> = {
    emerald: 'bg-emerald-500/10 text-emerald-400 hover:bg-emerald-500/20',
    blue: 'bg-blue-500/10 text-blue-400 hover:bg-blue-500/20',
    red: 'bg-red-500/10 text-red-400 hover:bg-red-500/20',
    orange: 'bg-orange-500/10 text-orange-400 hover:bg-orange-500/20',
  };
  return (
    <button onClick={onClick} disabled={loading}
      className={`px-2.5 py-1 rounded text-xs font-medium transition disabled:opacity-50 ${colors[color] || colors.blue}`}>
      {loading ? '...' : children}
    </button>
  );
}

function StateIcon({ state }: { state: string }) {
  const c: Record<string, string> = { warm: 'bg-emerald-400', allocated: 'bg-blue-400', booting: 'bg-yellow-400 animate-pulse', resetting: 'bg-orange-400 animate-pulse', error: 'bg-red-400', offline: 'bg-gray-600' };
  return <div className={`w-2.5 h-2.5 rounded-full flex-shrink-0 ${c[state] || 'bg-gray-600'}`} />;
}

function StateBadge({ state }: { state: string }) {
  const s: Record<string, string> = { warm: 'bg-emerald-400/10 text-emerald-400', allocated: 'bg-blue-400/10 text-blue-400', booting: 'bg-yellow-400/10 text-yellow-400', resetting: 'bg-orange-400/10 text-orange-400', error: 'bg-red-400/10 text-red-400', offline: 'bg-gray-700 text-gray-400' };
  return <span className={`text-xs px-2 py-0.5 rounded font-medium uppercase ${s[state] || s.offline}`}>{state}</span>;
}
