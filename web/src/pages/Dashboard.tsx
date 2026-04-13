import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api, PoolStatus, NodeHealth, DeviceInstance } from '../lib/api';

interface FederationNode {
  name: string;
  host: string;
  role: string;
  capacity?: number;
  warm?: number;
  allocated?: number;
  available?: number;
  healthy: boolean;
}

interface FederatedStatus {
  nodes: FederationNode[];
  total_nodes: number;
  total_capacity: number;
  total_allocated: number;
  total_available: number;
}

export function Dashboard() {
  const [pool, setPool] = useState<PoolStatus | null>(null);
  const [health, setHealth] = useState<NodeHealth | null>(null);
  const [avds, setAvds] = useState<string[]>([]);
  const [federation, setFederation] = useState<FederatedStatus | null>(null);
  const [remoteNodes, setRemoteNodes] = useState<Map<string, PoolStatus>>(new Map());
  const [error, setError] = useState('');
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const navigate = useNavigate();

  const refresh = async () => {
    try {
      const [p, h, a, fed] = await Promise.all([
        api.pool(), api.health(), api.avds(),
        api.federationStatus().catch(() => null),
      ]);
      setPool(p); setHealth(h); setAvds((a.avds || []).map((x: any) => x.name));
      setFederation(fed);

      // Fetch remote pools for peer nodes
      if (fed && fed.nodes) {
        const peerNodes = fed.nodes.filter((n: FederationNode) => n.role !== 'self' && n.healthy);
        const remoteData = new Map<string, PoolStatus>();
        await Promise.all(peerNodes.map(async (n: FederationNode) => {
          try {
            const rp = await api.remotePool(n.host);
            remoteData.set(n.host, rp);
          } catch {}
        }));
        setRemoteNodes(remoteData);
      }
      setError('');
    } catch (e: any) { setError(e.message); }
  };

  useEffect(() => { refresh(); const i = setInterval(refresh, 5000); return () => clearInterval(i); }, []);

  const handleBoot = async (avdName: string, node?: string) => {
    setActionLoading(avdName);
    try {
      if (node) { await api.remoteBoot(node, avdName); }
      else { await api.bootAVD(avdName); }
      refresh();
    } catch (e: any) { alert(e.message); }
    setActionLoading(null);
  };

  const handleShutdown = async (inst: DeviceInstance, node?: string) => {
    setActionLoading(inst.id);
    try {
      if (node) { await api.remoteShutdown(node, inst.id); }
      else { await api.shutdownInstance(inst.id); }
      refresh();
    } catch (e: any) { alert(e.message); }
    setActionLoading(null);
  };

  const handleRelease = async (sessionId: string) => {
    try { await api.releaseSession(sessionId); refresh(); } catch (e: any) { alert(e.message); }
  };

  if (error) return (
    <div className="text-center py-20">
      <div className="text-red-400 text-lg mb-2">Cannot connect to daemon</div>
      <div className="text-gray-500 text-sm">{error}</div>
    </div>
  );
  if (!pool || !health) return <div className="text-center py-20 text-gray-500">Connecting...</div>;

  const hasPeers = federation && federation.total_nodes > 1;
  const poolByName = new Map<string, DeviceInstance>();
  pool.instances.forEach(inst => poolByName.set(inst.device_name, inst));

  // Stats — federated if peers exist
  const stats = hasPeers ? [
    { label: 'Nodes', value: federation!.total_nodes, color: 'text-purple-400' },
    { label: 'Capacity', value: federation!.total_capacity + pool.total_capacity },
    { label: 'Available', value: federation!.total_available + pool.warm, color: 'text-emerald-400' },
    { label: 'Allocated', value: federation!.total_allocated + pool.allocated, color: 'text-blue-400' },
    { label: 'Booting', value: pool.booting, color: 'text-yellow-400' },
  ] : [
    { label: 'Capacity', value: pool.total_capacity },
    { label: 'Warm', value: pool.warm, color: 'text-emerald-400' },
    { label: 'Allocated', value: pool.allocated, color: 'text-blue-400' },
    { label: 'Booting', value: pool.booting, color: 'text-yellow-400' },
    { label: 'Error', value: pool.error, color: 'text-red-400' },
  ];

  const isFirstBoot = avds.length === 0 && pool.instances.length === 0;
  const hasAVDsButNoneRunning = avds.length > 0 && pool.instances.length === 0;

  // Quick-create: create AVDs using detected system images and boot them
  const [quickCreating, setQuickCreating] = useState(false);
  const [systemImages, setSystemImages] = useState<any[]>([]);
  const [quickCreateCount, setQuickCreateCount] = useState(Math.min(3, pool.total_capacity || 3));

  useEffect(() => {
    if (isFirstBoot) {
      api.systemImages().then((r: any) => setSystemImages(r.images || [])).catch(() => {});
    }
  }, [isFirstBoot]);

  const handleQuickCreate = async () => {
    setQuickCreating(true);
    try {
      const img = systemImages[0]; // best available image
      if (!img) { alert('No system images installed. Run: sdkmanager --install "system-images;android-35;google_apis;arm64-v8a"'); return; }
      await api.createAVDs({ system_image: img.path, device: 'pixel', count: quickCreateCount, profile_name: 'default' });
      await new Promise(r => setTimeout(r, 2000));
      refresh();
    } catch (e: any) { alert(e.message); }
    setQuickCreating(false);
  };

  const handleBootAll = async () => {
    setActionLoading('boot-all');
    for (const avd of avds) {
      try { await api.bootAVD(avd); } catch {}
    }
    setActionLoading(null);
    refresh();
  };

  return (
    <div className="space-y-6">
      {/* First Boot — Create Emulators */}
      {isFirstBoot && (
        <div className="bg-gradient-to-br from-gray-900 to-gray-800 rounded-xl border border-gray-700 p-8 text-center">
          <div className="text-4xl mb-4">📱</div>
          <h1 className="text-2xl font-bold text-white mb-2">Welcome to drizz-farm</h1>
          <p className="text-gray-400 mb-6 max-w-md mx-auto">
            Create emulators to start your device lab. They'll boot on-demand when tests need them.
          </p>

          {systemImages.length > 0 ? (
            <div className="max-w-sm mx-auto space-y-4">
              <div className="bg-gray-800/50 rounded-lg p-4 text-left">
                <div className="text-xs text-gray-500 mb-1">Detected system image:</div>
                <div className="text-sm text-emerald-400 font-mono">{systemImages[0].api_name} ({systemImages[0].variant})</div>
                <div className="flex items-center gap-3 mt-3">
                  <label className="text-xs text-gray-400">How many:</label>
                  <div className="flex gap-1">
                    {[1, 2, 3, 4, 5].filter(n => n <= (pool.total_capacity || 5)).map(n => (
                      <button key={n} onClick={() => setQuickCreateCount(n)}
                        className={`w-8 h-8 rounded text-sm font-medium transition ${
                          quickCreateCount === n ? 'bg-emerald-500 text-white' : 'bg-gray-700 text-gray-400 hover:bg-gray-600'
                        }`}>{n}</button>
                    ))}
                  </div>
                </div>
              </div>
              <button onClick={handleQuickCreate} disabled={quickCreating}
                className="w-full px-6 py-3 bg-emerald-500 text-white rounded-lg font-medium hover:bg-emerald-400 transition disabled:opacity-50">
                {quickCreating ? 'Creating...' : `Create ${quickCreateCount} Emulator${quickCreateCount > 1 ? 's' : ''}`}
              </button>
              <button onClick={() => navigate('/create')} className="text-xs text-gray-500 hover:text-emerald-400 transition">
                Advanced options
              </button>
            </div>
          ) : (
            <div className="max-w-md mx-auto space-y-3">
              <div className="text-sm text-yellow-400">No Android system images installed.</div>
              <code className="text-xs bg-gray-800 text-gray-300 px-3 py-2 rounded block">
                sdkmanager --install "system-images;android-35;google_apis;arm64-v8a"
              </code>
              <div className="text-xs text-gray-600">Then refresh this page.</div>
            </div>
          )}
        </div>
      )}

      {/* AVDs exist but none running */}
      {hasAVDsButNoneRunning && (
        <div className="bg-gray-900 rounded-lg border border-emerald-500/30 p-6 text-center">
          <div className="text-lg font-medium text-white mb-2">
            {avds.length} emulator{avds.length > 1 ? 's' : ''} ready to boot
          </div>
          <p className="text-gray-400 text-sm mb-4">
            Your emulators are created but not running. Boot them to start using your device lab.
          </p>
          <div className="flex gap-3 justify-center">
            <button onClick={handleBootAll} disabled={actionLoading === 'boot-all'}
              className="px-5 py-2 bg-emerald-500 text-white rounded-lg font-medium hover:bg-emerald-400 transition disabled:opacity-50">
              {actionLoading === 'boot-all' ? 'Booting...' : `Boot All (${avds.length})`}
            </button>
            <button onClick={() => navigate('/create')}
              className="px-5 py-2 bg-gray-700 text-gray-300 rounded-lg font-medium hover:bg-gray-600 transition">
              Create More
            </button>
          </div>
        </div>
      )}

      {/* Stats */}
      <div className="grid grid-cols-2 md:grid-cols-5 gap-3">
        {stats.map(s => (
          <div key={s.label} className="bg-gray-900 rounded-lg border border-gray-800 p-3">
            <div className="text-[10px] text-gray-500 uppercase tracking-wider">{s.label}</div>
            <div className={`text-2xl font-bold mt-1 ${s.color || 'text-gray-200'}`}>{s.value}</div>
          </div>
        ))}
      </div>

      {/* Node info */}
      <div className="bg-gray-900 rounded-lg border border-gray-800 p-3">
        <div className="flex items-center justify-between mb-2">
          <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wider">
            {hasPeers ? 'Cluster' : 'Node'}
          </h2>
          <span className="text-[10px] text-emerald-400 bg-emerald-400/10 px-1.5 py-0.5 rounded">{health.status}</span>
        </div>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3 text-xs">
          <div><span className="text-gray-500">Name:</span> <span className="text-gray-200">{health.node}</span></div>
          <div><span className="text-gray-500">Version:</span> <span className="text-gray-200">{health.version}</span></div>
          <div><span className="text-gray-500">Uptime:</span> <span className="text-gray-200">{health.uptime}</span></div>
          <div><span className="text-gray-500">CPUs:</span> <span className="text-gray-200">{health.resources.num_cpu}</span></div>
        </div>
      </div>

      {/* Local node devices */}
      <NodeDeviceList
        title={hasPeers ? `This Node (${health.node})` : 'Devices'}
        subtitle={`${avds.length} AVDs · ${pool.instances.filter(i => i.device_kind === 'android_usb').length} USB`}
        avds={avds}
        poolByName={poolByName}
        instances={pool.instances}
        actionLoading={actionLoading}
        isLeader={!hasPeers || federation?.nodes?.find(n => n.name === health.node || n.host === `${health.node}`)?.role === 'leader'}
        onBoot={(name) => handleBoot(name)}
        onShutdown={(inst) => handleShutdown(inst)}
        onRelease={handleRelease}
        onView={(id) => navigate(`/live/${id}`)}
      />

      {/* Peer nodes */}
      {hasPeers && federation!.nodes
        .filter(n => n.role !== 'self' && n.healthy)
        .map(node => {
          const remotePool = remoteNodes.get(node.host);
          const remoteByName = new Map<string, DeviceInstance>();
          (remotePool?.instances || []).forEach((inst: DeviceInstance) => remoteByName.set(inst.device_name, inst));

          return (
            <NodeDeviceList
              key={node.host}
              title={node.name || node.host}
              subtitle={`${node.capacity || 0} capacity · ${node.available || 0} available`}
              avds={[]}
              poolByName={remoteByName}
              instances={remotePool?.instances || []}
              actionLoading={actionLoading}
              onBoot={(name) => handleBoot(name, node.host)}
              onShutdown={(inst) => handleShutdown(inst, node.host)}
              onRelease={handleRelease}
              onView={(id) => navigate(`/live/${id}`)}
              isRemote
              nodeAddr={node.host}
            />
          );
        })
      }

      {/* Peer nodes that are down */}
      {hasPeers && federation!.nodes
        .filter(n => n.role !== 'self' && !n.healthy)
        .map(node => (
          <div key={node.host} className="bg-gray-900 rounded-lg border border-red-900/50 p-3">
            <div className="flex items-center justify-between">
              <span className="text-sm text-gray-400">{node.name || node.host}</span>
              <span className="text-[10px] text-red-400 bg-red-400/10 px-1.5 py-0.5 rounded">OFFLINE</span>
            </div>
          </div>
        ))
      }
    </div>
  );
}

// --- Sub-components ---

function NodeDeviceList({
  title, subtitle, avds, poolByName, instances, actionLoading,
  onBoot, onShutdown, onRelease, onView, isRemote, isLeader, nodeAddr: _nodeAddr
}: {
  title: string; subtitle: string;
  avds: string[]; poolByName: Map<string, DeviceInstance>; instances: DeviceInstance[];
  actionLoading: string | null;
  onBoot: (name: string) => void; onShutdown: (inst: DeviceInstance) => void;
  onRelease: (id: string) => void; onView: (id: string) => void;
  isRemote?: boolean; isLeader?: boolean; nodeAddr?: string;
}) {
  const stateColor: Record<string, string> = { warm: 'bg-emerald-400', allocated: 'bg-blue-400', booting: 'bg-yellow-400 animate-pulse', resetting: 'bg-orange-400 animate-pulse', error: 'bg-red-400', offline: 'bg-gray-600' };
  const badgeStyle: Record<string, string> = { warm: 'bg-emerald-400/10 text-emerald-400', allocated: 'bg-blue-400/10 text-blue-400', booting: 'bg-yellow-400/10 text-yellow-400', resetting: 'bg-orange-400/10 text-orange-400', error: 'bg-red-400/10 text-red-400', offline: 'bg-gray-700 text-gray-400' };

  // For remote nodes, show instances from pool directly
  // Build display: AVDs from disk + pool instances not matching any AVD + USB devices
  const avdSet = new Set(avds);
  const displayItems = isRemote
    ? instances.map(inst => ({ name: inst.device_name, inst, state: inst.state }))
    : [
        // AVDs from disk, overlaid with pool state
        ...avds.map(name => ({ name, inst: poolByName.get(name), state: poolByName.get(name)?.state || 'offline' })),
        // Pool instances not matching any AVD (adopted running emulators)
        ...instances.filter(i => !avdSet.has(i.device_name) && i.device_kind !== 'android_usb').map(inst => ({ name: inst.device_name, inst, state: inst.state })),
        // USB devices
        ...instances.filter(i => i.device_kind === 'android_usb').map(inst => ({ name: inst.device_name, inst, state: inst.state })),
      ];

  return (
    <div className="bg-gray-900 rounded-lg border border-gray-800">
      <div className="px-3 py-2 border-b border-gray-800 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wider">{title}</h2>
          <span className={`text-[9px] px-1.5 py-0.5 rounded ${
            isLeader ? 'bg-amber-400/10 text-amber-400' : 'bg-purple-400/10 text-purple-400'
          }`}>{isLeader ? 'LEADER' : 'NODE'}</span>
        </div>
        <span className="text-[10px] text-gray-600">{subtitle}</span>
      </div>

      <div className="divide-y divide-gray-800/50">
        {displayItems.length === 0 ? (
          <div className="p-4 text-center text-xs text-gray-600">No devices</div>
        ) : displayItems.map(({ name, inst, state }) => (
          <div key={name} className="px-3 py-2 flex items-center gap-3">
            <div className={`w-2 h-2 rounded-full flex-shrink-0 ${stateColor[state] || 'bg-gray-600'}`} />
            <div className="flex-1 min-w-0">
              <div className="text-xs font-medium text-gray-200 truncate">{name}</div>
              <div className="text-[10px] text-gray-600">
                {inst ? `${inst.node_name ? inst.node_name + ':' : ''}${inst.serial} · port:${inst.connection?.adb_port || '-'}` : 'offline'}
              </div>
            </div>
            <span className={`text-[9px] px-1.5 py-0.5 rounded font-medium uppercase ${badgeStyle[state] || badgeStyle.offline}`}>{state}</span>
            <div className="flex gap-1">
              {(state === 'warm' || state === 'allocated') && inst && (
                <Btn onClick={() => onView(inst.id)} color="blue">View</Btn>
              )}
              {state === 'offline' && (
                <Btn onClick={() => onBoot(name)} loading={actionLoading === name} color="emerald">Start</Btn>
              )}
              {state === 'warm' && inst && (
                <Btn onClick={() => onShutdown(inst)} loading={actionLoading === inst.id} color="red">Stop</Btn>
              )}
              {state === 'allocated' && inst?.session_id && (
                <Btn onClick={() => onRelease(inst.session_id!)} color="orange">Release</Btn>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function Btn({ onClick, loading, color, children }: { onClick: () => void; loading?: boolean; color: string; children: React.ReactNode }) {
  const colors: Record<string, string> = {
    emerald: 'bg-emerald-500/10 text-emerald-400 hover:bg-emerald-500/20',
    blue: 'bg-blue-500/10 text-blue-400 hover:bg-blue-500/20',
    red: 'bg-red-500/10 text-red-400 hover:bg-red-500/20',
    orange: 'bg-orange-500/10 text-orange-400 hover:bg-orange-500/20',
  };
  return (
    <button onClick={onClick} disabled={loading}
      className={`px-2 py-0.5 rounded text-[9px] font-medium transition disabled:opacity-50 ${colors[color]}`}>
      {loading ? '...' : children}
    </button>
  );
}
