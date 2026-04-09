import { useEffect, useState } from 'react';
import { api, PoolStatus, NodeHealth } from '../lib/api';

export function Dashboard() {
  const [pool, setPool] = useState<PoolStatus | null>(null);
  const [health, setHealth] = useState<NodeHealth | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    const refresh = async () => {
      try {
        const [p, h] = await Promise.all([api.pool(), api.health()]);
        setPool(p); setHealth(h); setError('');
      } catch (e: any) { setError(e.message); }
    };
    refresh();
    const i = setInterval(refresh, 3000);
    return () => clearInterval(i);
  }, []);

  if (error) return <div className="text-center py-20"><div className="text-red-400 text-lg mb-2">Cannot connect to daemon</div><div className="text-gray-500 text-sm">{error}</div></div>;
  if (!pool || !health) return <div className="text-center py-20 text-gray-500">Connecting...</div>;

  const stats = [
    { label: 'Capacity', value: pool.total_capacity },
    { label: 'Warm', value: pool.warm, color: 'text-emerald-400' },
    { label: 'Allocated', value: pool.allocated, color: 'text-blue-400' },
    { label: 'Booting', value: pool.booting, color: 'text-yellow-400' },
    { label: 'Error', value: pool.error, color: 'text-red-400' },
  ];

  const stateColor: Record<string, string> = { warm: 'bg-emerald-400', allocated: 'bg-blue-400', booting: 'bg-yellow-400 animate-pulse', resetting: 'bg-orange-400 animate-pulse', error: 'bg-red-400' };
  const badgeStyle: Record<string, string> = { warm: 'bg-emerald-400/10 text-emerald-400', allocated: 'bg-blue-400/10 text-blue-400', booting: 'bg-yellow-400/10 text-yellow-400', resetting: 'bg-orange-400/10 text-orange-400', error: 'bg-red-400/10 text-red-400' };

  return (
    <div className="space-y-8">
      <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
        {stats.map(s => (
          <div key={s.label} className="bg-gray-900 rounded-lg border border-gray-800 p-4">
            <div className="text-xs text-gray-500 uppercase tracking-wider">{s.label}</div>
            <div className={`text-2xl font-bold mt-1 ${s.color || 'text-gray-200'}`}>{s.value}</div>
          </div>
        ))}
      </div>

      <div className="bg-gray-900 rounded-lg border border-gray-800 p-4">
        <h2 className="text-sm font-semibold text-gray-400 uppercase tracking-wider mb-3">Node</h2>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
          <div><span className="text-gray-500">Name:</span> <span className="text-gray-200">{health.node}</span></div>
          <div><span className="text-gray-500">Version:</span> <span className="text-gray-200">{health.version}</span></div>
          <div><span className="text-gray-500">Uptime:</span> <span className="text-gray-200">{health.uptime}</span></div>
          <div><span className="text-gray-500">CPUs:</span> <span className="text-gray-200">{health.resources.num_cpu}</span></div>
        </div>
      </div>

      <div className="bg-gray-900 rounded-lg border border-gray-800">
        <div className="px-4 py-3 border-b border-gray-800">
          <h2 className="text-sm font-semibold text-gray-400 uppercase tracking-wider">Devices</h2>
        </div>
        {pool.instances.length === 0 ? (
          <div className="p-8 text-center text-gray-500">No devices in pool. Create a session to boot one on-demand.</div>
        ) : (
          <div className="divide-y divide-gray-800">
            {pool.instances.map(inst => (
              <div key={inst.id} className="px-4 py-3 flex items-center gap-4">
                <div className={`w-2.5 h-2.5 rounded-full ${stateColor[inst.state] || 'bg-gray-600'}`} />
                <div className="flex-1 min-w-0">
                  <div className="text-sm font-medium text-gray-200 truncate">{inst.device_name || inst.serial}</div>
                  <div className="text-xs text-gray-500">{inst.device_kind} · {inst.serial}{inst.connection.adb_port ? ` · port:${inst.connection.adb_port}` : ''}</div>
                </div>
                <div className="text-right">
                  <span className={`text-xs px-2 py-0.5 rounded font-medium uppercase ${badgeStyle[inst.state] || 'bg-gray-700 text-gray-400'}`}>{inst.state}</span>
                  {inst.session_id && <div className="text-xs text-gray-500 mt-0.5">session: {inst.session_id}</div>}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
