import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Server, Wifi, Plus, Usb, Apple } from 'lucide-react';
import { api, PoolStatus, NodeHealth, DeviceInstance } from '../lib/api';
import { StatusBadge, StatusDot } from '../components/StatusBadge';
import { ActionButton } from '../components/ActionButton';
import { CreateWizard } from './CreateWizard';

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

function formatUptime(raw: string): string {
  // Parse Go duration like "1h33m45.123s" or "3h22m1.5s"
  const h = raw.match(/(\d+)h/); const m = raw.match(/(\d+)m/); const s = raw.match(/([\d.]+)s/);
  const totalSec = (h ? parseInt(h[1]) * 3600 : 0) + (m ? parseInt(m[1]) * 60 : 0) + (s ? parseFloat(s[1]) : 0);
  const days = Math.floor(totalSec / 86400);
  const hours = Math.floor((totalSec % 86400) / 3600);
  const mins = Math.floor((totalSec % 3600) / 60);
  if (days > 0) return `${days}d ${hours}h ${mins}m`;
  if (hours > 0) return `${hours}h ${mins}m`;
  return `${mins}m`;
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
  const [systemImages, setSystemImages] = useState<any[]>([]);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const navigate = useNavigate();

  const refresh = async () => {
    try {
      const [p, h, a, fed] = await Promise.all([
        api.pool(), api.health(),
        api.avds().catch(() => ({ avds: [] })),
        api.federationStatus().catch(() => null),
      ]);
      setPool(p); setHealth(h);
      const avdList = a.avds || [];
      setAvds(avdList.map((x: any) => x.name));
      const infoMap = new Map<string, { display_name: string }>();
      avdList.forEach((x: any) => infoMap.set(x.name, { display_name: x.display_name || '' }));
      setAvdInfoMap(infoMap);
      if ((a.avds || []).length === 0 && p.instances.length === 0) {
        api.systemImages().then((r: any) => setSystemImages(r.images || [])).catch(() => {});
      }
      setFederation(fed);
      if (fed && fed.nodes) {
        const peerNodes = fed.nodes.filter((n: FederationNode) => n.role !== 'self' && n.healthy);
        const remoteData = new Map<string, PoolStatus>();
        await Promise.all(peerNodes.map(async (n: FederationNode) => {
          try { const rp = await api.remotePool(n.host); remoteData.set(n.host, rp); } catch {}
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
      if (node) await api.remoteBoot(node, avdName);
      else await api.bootAVD(avdName);
      refresh();
    } catch (e: any) { alert(e.message); }
    setActionLoading(null);
  };

  const handleShutdown = async (inst: DeviceInstance, node?: string) => {
    setActionLoading(inst.id);
    try {
      if (node) await api.remoteShutdown(node, inst.id);
      else await api.shutdownInstance(inst.id);
      refresh();
    } catch (e: any) { alert(e.message); }
    setActionLoading(null);
  };

  const handleRelease = async (sessionId: string) => {
    try { await api.releaseSession(sessionId); refresh(); } catch (e: any) { alert(e.message); }
  };

  if (error) return (
    <div className="flex flex-col items-center justify-center py-24 animate-fade-in">
      <div className="w-12 h-12 rounded-full flex items-center justify-center mb-4" style={{ background: 'hsl(var(--destructive) / 0.1)' }}>
        <span className="text-destructive text-xl">!</span>
      </div>
      <div className="text-destructive text-base font-medium mb-1">Cannot connect to daemon</div>
      <div className="text-muted-foreground text-sm font-mono">{error}</div>
    </div>
  );

  if (!pool || !health) return (
    <div className="flex items-center justify-center py-24">
      <div className="w-6 h-6 border-2 border-muted border-t-primary rounded-full animate-spin" />
      <span className="ml-3 text-muted-foreground text-sm">Connecting...</span>
    </div>
  );

  const hasPeers = federation && federation.total_nodes > 1;
  const poolByName = new Map<string, DeviceInstance>();
  pool.instances.forEach(inst => poolByName.set(inst.device_name, inst));

  const nodeCount = hasPeers ? federation!.total_nodes : 1;
  const stats = [
    { label: 'Nodes', value: nodeCount, color: 'text-purple-400' },
    { label: 'Capacity', value: hasPeers ? federation!.total_capacity + pool.total_capacity : pool.total_capacity },
    { label: 'Available', value: hasPeers ? federation!.total_available + pool.warm : pool.warm, color: 'text-primary' },
    { label: 'Allocated', value: hasPeers ? federation!.total_allocated + pool.allocated : pool.allocated, color: 'text-accent' },
    { label: 'Booting', value: pool.booting, color: 'text-status-booting' },
    { label: 'Error', value: pool.error, color: 'text-destructive' },
  ];

  return (
    <div className="space-y-5 animate-fade-in">
      {/* Stats header */}
      <div className="section-card">
        <div className="section-header">
          <div className="flex items-center gap-3">
            <div className="w-8 h-8 rounded-lg flex items-center justify-center" style={{ background: 'hsl(var(--primary) / 0.1)' }}>
              <Wifi className="w-4 h-4 text-primary" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h1 className="text-sm font-semibold text-foreground">{health.mesh?.name || health.node}</h1>
                <StatusDot state="running" />
              </div>
              <div className="text-[11px] text-muted-foreground font-mono mt-0.5">
                v{health.version} · {health.resources.num_cpu} CPUs · {formatUptime(health.uptime)}
              </div>
            </div>
          </div>
        </div>
        <div className="grid grid-cols-6 divide-x divide-border">
          {stats.map(s => (
            <div key={s.label} className="stat-card">
              <div className="stat-label">{s.label}</div>
              <div className={`stat-value ${s.color || 'text-foreground'}`}>{s.value}</div>
            </div>
          ))}
        </div>
      </div>

      {/* Local node */}
      <NodeDeviceList
        title={health.node}
        subtitle={`${avds.length} AVDs · ${pool.instances.filter(i => i.device_kind === 'android_usb').length} USB`}
        avds={avds}
        avdInfoMap={avdInfoMap}
        poolByName={poolByName}
        instances={pool.instances}
        actionLoading={actionLoading}
        isLeader={!hasPeers || federation?.nodes?.find(n => n.name === health.node)?.role === 'leader'}
        onBoot={(name) => handleBoot(name)}
        onShutdown={(inst) => handleShutdown(inst)}
        onRelease={handleRelease}
        onView={(id) => navigate(`/live/${id}`)}
        onAdd={() => setShowCreateModal(true)}
      />

      {/* Peer nodes */}
      {hasPeers && federation!.nodes
        .filter(n => n.role !== 'self' && n.healthy)
        .map(node => {
          const remotePool = remoteNodes.get(node.host);
          const remoteByName = new Map<string, DeviceInstance>();
          (remotePool?.instances || []).forEach((inst: DeviceInstance) => remoteByName.set(inst.device_name, inst));
          return (
            <NodeDeviceList key={node.host} title={node.name || node.host}
              subtitle={`${node.capacity || 0} capacity · ${node.available || 0} available`}
              avds={[]} poolByName={remoteByName} instances={remotePool?.instances || []}
              actionLoading={actionLoading}
              onBoot={(name) => handleBoot(name, node.host)}
              onShutdown={(inst) => handleShutdown(inst, node.host)}
              onRelease={handleRelease} onView={(id) => navigate(`/live/${id}`)}
              isRemote nodeAddr={node.host}
            />
          );
        })}

      {/* Offline peers */}
      {hasPeers && federation!.nodes
        .filter(n => n.role !== 'self' && !n.healthy)
        .map(node => (
          <div key={node.host} className="section-card p-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <Server className="w-3.5 h-3.5 text-muted-foreground" />
                <span className="text-sm text-muted-foreground">{node.name || node.host}</span>
              </div>
              <span className="badge badge-error">OFFLINE</span>
            </div>
          </div>
        ))}

      {/* Slide-out create panel */}
      <div className={`fixed inset-y-0 right-0 z-50 transition-transform duration-300 ease-in-out ${showCreateModal ? 'translate-x-0' : 'translate-x-full'}`}
        style={{ width: 'min(560px, 90vw)' }}>
        {showCreateModal && (
          <div className="fixed inset-0 -z-10" style={{ background: 'hsl(var(--surface-0) / 0.6)' }}
            onClick={() => setShowCreateModal(false)} />
        )}
        <div className="h-full surface-1 border-l border-border flex flex-col shadow-2xl">
          <div className="flex-1 overflow-y-auto p-6">
            {showCreateModal && (
              <CreateWizard isModal onClose={() => { setShowCreateModal(false); refresh(); }} />
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// --- Sub-components ---

function DeviceIcon({ kind }: { kind?: string }) {
  if (kind === 'android_usb') return <Usb className="w-3.5 h-3.5 text-status-booting flex-shrink-0" />;
  if (kind === 'ios_simulator' || kind === 'ios_usb') return <Apple className="w-3.5 h-3.5 text-foreground flex-shrink-0" />;
  return (
    <svg className="w-3.5 h-3.5 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="hsl(var(--primary))" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <rect x="5" y="2" width="14" height="20" rx="2" />
      <path d="M12 18h.01" />
    </svg>
  );
}

function NodeDeviceList({
  title, subtitle, avds, avdInfoMap, poolByName, instances, actionLoading,
  onBoot, onShutdown, onRelease, onView, onAdd, isRemote, isLeader, nodeAddr: _nodeAddr
}: {
  title: string; subtitle: string;
  avds: string[]; avdInfoMap?: Map<string, { display_name: string }>; poolByName: Map<string, DeviceInstance>; instances: DeviceInstance[];
  actionLoading: string | null;
  onBoot: (name: string) => void; onShutdown: (inst: DeviceInstance) => void;
  onRelease: (id: string) => void; onView: (id: string) => void;
  onAdd?: () => void; isRemote?: boolean; isLeader?: boolean; nodeAddr?: string;
}) {
  const avdSet = new Set(avds);
  const displayItems = isRemote
    ? instances.map(inst => ({ name: inst.device_name, inst, state: inst.state }))
    : [
        ...avds.map(name => ({ name, inst: poolByName.get(name), state: poolByName.get(name)?.state || 'offline' })),
        ...instances.filter(i => !avdSet.has(i.device_name) && i.device_kind !== 'android_usb').map(inst => ({ name: inst.device_name, inst, state: inst.state })),
        ...instances.filter(i => i.device_kind === 'android_usb').map(inst => ({ name: inst.device_name, inst, state: inst.state })),
      ];

  return (
    <div className="section-card">
      <div className="section-header">
        <div className="flex items-center gap-2.5">
          <Server className="w-3.5 h-3.5 text-muted-foreground" />
          <h2 className="text-[13px] font-semibold text-foreground">{title}</h2>
          <span className={`badge ${isLeader ? 'badge-leader' : 'badge-node'}`}>
            {isLeader ? 'LEADER' : 'NODE'}
          </span>
        </div>
        <div className="flex items-center gap-3">
          <span className="text-[11px] text-muted-foreground font-mono">{subtitle}</span>
          {!isRemote && onAdd && (
            <button onClick={onAdd} className="action-btn flex items-center gap-1.5 bg-[hsl(var(--primary))] text-primary-foreground hover:bg-[hsl(var(--primary))]/90 shadow-sm" style={{ boxShadow: '0 0 12px -2px hsl(var(--primary) / 0.3)' }}>
              <Plus className="w-3 h-3" /> Add
            </button>
          )}
        </div>
      </div>

      <div className="divide-y divide-border/50">
        {displayItems.length === 0 ? (
          <div className="p-8 text-center text-sm text-muted-foreground">No devices</div>
        ) : displayItems.map(({ name, inst, state }) => (
          <div key={name} className="px-5 py-3 flex items-center gap-4 card-hover group animate-slide-in">
            <DeviceIcon kind={inst?.device_kind} />
            <div className="flex-1 min-w-0">
              <div className="text-[13px] font-medium text-foreground truncate font-mono">{name}</div>
              {(inst?.display_info || avdInfoMap?.get(name)?.display_name) && (
                <div className="text-[11px] text-purple-400 truncate mt-0.5">
                  {inst?.display_info || avdInfoMap?.get(name)?.display_name || ''}
                </div>
              )}
              {inst && (
                <div className="text-[10px] text-muted-foreground/60 font-mono mt-0.5">
                  {inst.serial}:{inst.connection?.adb_port || '-'}
                </div>
              )}
            </div>
            <StatusBadge state={state} />
            <div className="flex gap-1.5 opacity-80 group-hover:opacity-100 transition-opacity">
              {(state === 'warm' || state === 'allocated') && inst && (
                <ActionButton onClick={() => onView(inst.id)} variant="accent">View</ActionButton>
              )}
              {state === 'offline' && (
                <ActionButton onClick={() => onBoot(name)} loading={actionLoading === name} variant="primary">Start</ActionButton>
              )}
              {state === 'warm' && inst && (
                <ActionButton onClick={() => onShutdown(inst)} loading={actionLoading === inst.id} variant="danger">Stop</ActionButton>
              )}
              {state === 'allocated' && inst?.session_id && (
                <ActionButton onClick={() => onRelease(inst.session_id!)} variant="warning">Release</ActionButton>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
