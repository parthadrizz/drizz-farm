import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Server, Smartphone, Tablet, Apple, Usb, Wifi, Plus } from 'lucide-react';
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
  const [avdInfoMap, setAvdInfoMap] = useState<Map<string, { display_name: string }>>(new Map());
  const [federation, setFederation] = useState<FederatedStatus | null>(null);
  const [remoteNodes, setRemoteNodes] = useState<Map<string, PoolStatus>>(new Map());
  const [error, setError] = useState('');
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [quickCreating, setQuickCreating] = useState(false);
  const [systemImages, setSystemImages] = useState<any[]>([]);
  const [quickCreateCount, setQuickCreateCount] = useState(3);
  const navigate = useNavigate();

  const refresh = async () => {
    try {
      const [p, h, a, fed] = await Promise.all([
        api.pool(), api.health(), api.avds(),
        api.federationStatus().catch(() => null),
      ]);
      setPool(p); setHealth(h);
      const avdList = a.avds || [];
      setAvds(avdList.map((x: any) => x.name));
      const infoMap = new Map<string, { display_name: string }>();
      avdList.forEach((x: any) => infoMap.set(x.name, { display_name: x.display_name || '' }));
      setAvdInfoMap(infoMap);
      // Fetch system images for first-boot emulator creation
      if ((a.avds || []).length === 0 && p.instances.length === 0) {
        api.systemImages().then((r: any) => setSystemImages(r.images || [])).catch(() => {});
      }
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
      <div className="text-[hsl(215,10%,50%)] text-sm">{error}</div>
    </div>
  );
  if (!pool || !health) return <div className="text-center py-20 text-[hsl(215,10%,50%)]">Connecting...</div>;

  const hasPeers = federation && federation.total_nodes > 1;
  const poolByName = new Map<string, DeviceInstance>();
  pool.instances.forEach(inst => poolByName.set(inst.device_name, inst));

  // Stats — federated if peers exist
  const stats = hasPeers ? [
    { label: 'Nodes', value: federation!.total_nodes, color: 'text-purple-400' },
    { label: 'Capacity', value: federation!.total_capacity + pool.total_capacity },
    { label: 'Available', value: federation!.total_available + pool.warm, color: 'text-[hsl(150,70%,42%)]' },
    { label: 'Allocated', value: federation!.total_allocated + pool.allocated, color: 'text-blue-400' },
    { label: 'Booting', value: pool.booting, color: 'text-yellow-400' },
  ] : [
    { label: 'Capacity', value: pool.total_capacity },
    { label: 'Warm', value: pool.warm, color: 'text-[hsl(150,70%,42%)]' },
    { label: 'Allocated', value: pool.allocated, color: 'text-blue-400' },
    { label: 'Booting', value: pool.booting, color: 'text-yellow-400' },
    { label: 'Error', value: pool.error, color: 'text-red-400' },
  ];

  const isFirstBoot = avds.length === 0 && pool.instances.length === 0;
  const hasAVDsButNoneRunning = avds.length > 0 && pool.instances.length === 0;

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
    <div className="space-y-4">
      {/* Header */}
      <div className="surface-1 rounded-lg border border-[hsl(215,14%,18%)] overflow-hidden">
        <div className="px-4 py-3 flex items-center justify-between border-b border-[hsl(215,14%,18%)]">
          <div className="flex items-center gap-3">
            <Wifi className="w-4 h-4 text-[hsl(150,70%,42%)]" />
            <div>
              <div className="flex items-center gap-2">
                <h1 className="text-[15px] font-bold text-white">{health.mesh?.name || health.node}</h1>
                <span className="status-dot status-dot-running" />
              </div>
            </div>
          </div>
        </div>
        <div className="grid grid-cols-5 divide-x divide-[hsl(215,14%,18%)]">
          {stats.map(s => (
            <div key={s.label} className="px-4 py-2.5">
              <div className="text-[9px] text-[hsl(215,10%,45%)] uppercase tracking-wider font-semibold">{s.label}</div>
              <div className={`text-lg font-bold font-mono mt-0.5 ${s.color || 'text-white'}`}>{s.value}</div>
            </div>
          ))}
        </div>
      </div>

      {/* Node info merged into device list below */}

      {/* Local node devices */}
      <NodeDeviceList
        title={health.node}
        subtitle={`${avds.length} AVDs · ${pool.instances.filter(i => i.device_kind === 'android_usb').length} USB · v${health.version} · ${health.resources.num_cpu} CPUs`}
        avds={avds}
        avdInfoMap={avdInfoMap}
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
          <div key={node.host} className="surface-1 rounded-lg border border-red-900/50 p-3">
            <div className="flex items-center justify-between">
              <span className="text-sm text-[hsl(215,10%,55%)]">{node.name || node.host}</span>
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
  title, subtitle, avds, avdInfoMap, poolByName, instances, actionLoading,
  onBoot, onShutdown, onRelease, onView, isRemote, isLeader, nodeAddr: _nodeAddr
}: {
  title: string; subtitle: string;
  avds: string[]; avdInfoMap?: Map<string, { display_name: string }>; poolByName: Map<string, DeviceInstance>; instances: DeviceInstance[];
  actionLoading: string | null;
  onBoot: (name: string) => void; onShutdown: (inst: DeviceInstance) => void;
  onRelease: (id: string) => void; onView: (id: string) => void;
  isRemote?: boolean; isLeader?: boolean; nodeAddr?: string;
}) {
  const dotStyle: Record<string, string> = {
    warm: 'status-dot-running', allocated: 'status-dot-paused',
    booting: 'status-dot-paused animate-pulse', resetting: 'status-dot-paused animate-pulse',
    error: 'status-dot-blocked', offline: 'status-dot-idle',
  };
  const badgeStyle: Record<string, string> = {
    warm: 'bg-[hsl(150,70%,42%)]/10 text-[hsl(150,70%,42%)] border border-[hsl(150,70%,42%)]/20',
    allocated: 'bg-[hsl(212,92%,67%)]/10 text-[hsl(212,92%,67%)] border border-[hsl(212,92%,67%)]/20',
    booting: 'bg-[hsl(38,90%,50%)]/10 text-[hsl(38,90%,50%)] border border-[hsl(38,90%,50%)]/20',
    resetting: 'bg-[hsl(38,90%,50%)]/10 text-[hsl(38,90%,50%)] border border-[hsl(38,90%,50%)]/20',
    error: 'bg-[hsl(4,75%,55%)]/10 text-[hsl(4,75%,55%)] border border-[hsl(4,75%,55%)]/20',
    offline: 'surface-3 text-[hsl(215,10%,50%)] border border-[hsl(215,14%,18%)]',
  };

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
    <div className="surface-1 rounded-lg border border-[hsl(215,14%,18%)]">
      <div className="px-4 py-2.5 border-b border-[hsl(215,14%,18%)] flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Server className="w-3.5 h-3.5 text-[hsl(215,10%,50%)]" />
          <h2 className="text-[13px] font-semibold text-white">{title}</h2>
          <span className={`text-[9px] font-medium px-1.5 py-0.5 rounded-full ${
            isLeader
              ? 'bg-[hsl(38,90%,50%)]/10 text-[hsl(38,90%,50%)] border border-[hsl(38,90%,50%)]/20'
              : 'surface-2 text-[hsl(215,10%,50%)] border border-[hsl(215,14%,18%)]'
          }`}>{isLeader ? 'LEADER' : 'NODE'}</span>
        </div>
        <div className="flex items-center gap-3">
          <span className="text-[11px] text-[hsl(215,10%,50%)] font-mono">{subtitle}</span>
          {!isRemote && <button onClick={() => window.location.href = '/create'} className="flex items-center gap-1 px-2.5 py-1 bg-[hsl(150,70%,42%)]/10 text-[hsl(150,70%,42%)] rounded-md text-[10px] font-semibold hover:bg-[hsl(150,70%,42%)]/20 transition border border-[hsl(150,70%,42%)]/20">
            <Plus className="w-3 h-3" /> Add
          </button>}
        </div>
      </div>

      <div className="divide-y divide-[hsl(215,14%,18%)]/50">
        {displayItems.length === 0 ? (
          <div className="p-6 text-center text-[13px] text-[hsl(215,10%,50%)]">No devices</div>
        ) : displayItems.map(({ name, inst, state }) => (
          <div key={name} className="px-4 py-2.5 flex items-center gap-3 hover:surface-2 transition-colors">
            {/* Platform + type icon */}
            {inst?.device_kind === 'android_usb' ? (
              <Usb className="w-3.5 h-3.5 text-[hsl(38,90%,50%)] flex-shrink-0" />
            ) : inst?.device_kind === 'ios_simulator' || inst?.device_kind === 'ios_usb' ? (
              <Apple className="w-3.5 h-3.5 text-[hsl(210,14%,73%)] flex-shrink-0" />
            ) : (
              <svg className="w-3.5 h-3.5 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="hsl(150,70%,42%)" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <rect x="5" y="2" width="14" height="20" rx="2" />
                <path d="M12 18h.01" />
              </svg>
            )}
            <div className="flex-1 min-w-0">
              <div className="text-[13px] font-medium text-white truncate font-mono">{name}</div>
              <div className="text-[11px] text-[hsl(270,60%,70%)] truncate">
                {inst?.display_info || avdInfoMap?.get(name)?.display_name || ''}
              </div>
              {inst && (
                <div className="text-[10px] text-[hsl(215,10%,40%)] font-mono">
                  {inst.serial}:{inst.connection?.adb_port || '-'}
                </div>
              )}
            </div>
            <span className={`text-[9px] px-2 py-0.5 rounded-full font-semibold uppercase ${badgeStyle[state] || badgeStyle.offline}`}>{state}</span>
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
    emerald: 'bg-[hsl(150,70%,42%)]/10 text-[hsl(150,70%,42%)] hover:bg-[hsl(150,70%,42%)]/20 border border-[hsl(150,70%,42%)]/20',
    blue: 'bg-[hsl(212,92%,67%)]/10 text-[hsl(212,92%,67%)] hover:bg-[hsl(212,92%,67%)]/20 border border-[hsl(212,92%,67%)]/20',
    red: 'bg-[hsl(4,75%,55%)]/10 text-[hsl(4,75%,55%)] hover:bg-[hsl(4,75%,55%)]/20 border border-[hsl(4,75%,55%)]/20',
    orange: 'bg-[hsl(38,90%,50%)]/10 text-[hsl(38,90%,50%)] hover:bg-[hsl(38,90%,50%)]/20 border border-[hsl(38,90%,50%)]/20',
  };
  return (
    <button onClick={onClick} disabled={loading}
      className={`px-2.5 py-1 rounded-md text-[10px] font-semibold transition disabled:opacity-50 ${colors[color]}`}>
      {loading ? '...' : children}
    </button>
  );
}
