import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Server, Wifi, Plus, Smartphone, Lock, Unlock } from 'lucide-react';
import { api, PoolStatus, NodeHealth, DeviceInstance, NodeEntry } from '../lib/api';
import { StatusBadge } from '../components/StatusBadge';
import { ActionButton } from '../components/ActionButton';
import { EmptyState } from '../components/EmptyState';
import { Kebab, KebabItem } from '../components/Kebab';
import { CreateWizard } from './CreateWizard';

// Per-node snapshot the dashboard assembles by talking to each node directly.
interface NodeSnapshot {
  entry: NodeEntry;          // name + url
  online: boolean;
  pool?: PoolStatus;
  health?: NodeHealth;
  avds?: { name: string; display_name?: string }[];
  error?: string;
}

function formatUptime(raw: string): string {
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
  const [nodes, setNodes] = useState<NodeSnapshot[]>([]);
  const [groupName, setGroupName] = useState<string>('');
  const [selfName, setSelfName] = useState<string>('');
  const [error, setError] = useState('');
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const navigate = useNavigate();

  const refresh = async () => {
    try {
      // 1. Ask this node for the group + node list
      const [list, self] = await Promise.all([api.listNodes(), api.groupInfo()]);
      setGroupName(list.group_name || '');
      setSelfName(self.self.name);

      // 2. Fetch each node's state in parallel, browser-side. No backend proxying.
      const snapshots = await Promise.all(
        list.nodes.map(async (entry): Promise<NodeSnapshot> => {
          try {
            const [pool, health, avdsResp] = await Promise.all([
              api.peer.pool(entry.url),
              api.peer.health(entry.url),
              api.peer.avds(entry.url).catch(() => ({ avds: [] })),
            ]);
            return { entry, online: true, pool, health, avds: avdsResp.avds || [] };
          } catch (e: any) {
            return { entry, online: false, error: e?.message || 'unreachable' };
          }
        })
      );

      setNodes(snapshots);
      setError('');
    } catch (e: any) {
      setError(e.message);
    }
  };

  useEffect(() => {
    refresh();
    const i = setInterval(refresh, 5000);
    return () => clearInterval(i);
  }, []);

  // Boot an AVD on a specific node (the node that owns it).
  const handleBoot = async (node: NodeSnapshot, avdName: string) => {
    setActionLoading(avdName);
    try {
      await api.peer.boot(node.entry.url, avdName);
      refresh();
    } catch (e: any) { alert(e.message); }
    setActionLoading(null);
  };

  const handleShutdown = async (node: NodeSnapshot, inst: DeviceInstance) => {
    setActionLoading(inst.id);
    try {
      await api.peer.shutdown(node.entry.url, inst.id);
      refresh();
    } catch (e: any) { alert(e.message); }
    setActionLoading(null);
  };

  if (error && nodes.length === 0) return (
    <div className="flex flex-col items-center justify-center py-24 animate-fade-in">
      <div className="w-12 h-12 rounded-full flex items-center justify-center mb-4" style={{ background: 'hsl(var(--destructive) / 0.1)' }}>
        <span className="text-destructive text-xl">!</span>
      </div>
      <div className="text-destructive text-base font-medium mb-1">Cannot connect to daemon</div>
      <div className="text-muted-foreground text-sm font-mono">{error}</div>
    </div>
  );

  if (nodes.length === 0) return (
    <div className="flex items-center justify-center py-24">
      <div className="w-6 h-6 border-2 border-muted border-t-primary rounded-full animate-spin" />
      <span className="ml-3 text-muted-foreground text-sm">Connecting...</span>
    </div>
  );

  // Aggregate stats across all online nodes.
  // Capacity = actual devices (AVDs + USB), not the config slot limit.
  let totalDevices = 0;
  let totalAvailable = 0;
  let totalAllocated = 0;
  let totalBooting = 0;
  let totalError = 0;

  nodes.forEach(n => {
    if (!n.online || !n.pool) return;
    const avds = n.avds?.length || 0;
    const usb = n.pool.instances.filter(i => i.device_kind === 'android_usb').length;
    const devices = avds + usb;
    const cap = Math.min(n.pool.total_capacity, devices);
    totalDevices += cap;
    totalAllocated += n.pool.allocated;
    totalBooting += n.pool.booting;
    totalError += n.pool.error;
    totalAvailable += Math.max(0, cap - n.pool.allocated - n.pool.booting);
  });

  const onlineCount = nodes.filter(n => n.online).length;

  const stats = [
    { label: 'Nodes', value: onlineCount, color: 'text-purple-400' },
    { label: 'Capacity', value: totalDevices },
    { label: 'Available', value: totalAvailable, color: 'text-primary' },
    { label: 'Allocated', value: totalAllocated, color: 'text-accent' },
    { label: 'Booting', value: totalBooting, color: 'text-status-booting' },
    { label: 'Error', value: totalError, color: 'text-destructive' },
  ];

  const selfHealth = nodes.find(n => n.entry.name === selfName)?.health;

  // First-run hero: nothing exists yet on any node. Replace the
  // sad-blank-table experience with a friendly welcome + CTA.
  const totalAVDsAcrossNodes = nodes.reduce((acc, n) => acc + (n.avds?.length || 0), 0);
  const totalUSBAcrossNodes = nodes.reduce(
    (acc, n) => acc + (n.pool?.instances.filter(i => i.device_kind === 'android_usb').length || 0),
    0,
  );
  if (totalAVDsAcrossNodes === 0 && totalUSBAcrossNodes === 0) {
    return (
      <div className="space-y-5 animate-fade-in">
        <div className="section-card">
          <EmptyState
            icon={Smartphone}
            title="Welcome to drizz-farm"
            description={
              <>
                You don't have any emulators yet. Create your first one to start running
                tests, recording sessions, and streaming devices to your team.
              </>
            }
            primary={{ label: 'Create your first emulator', icon: Plus, onClick: () => setShowCreateModal(true) }}
            secondary={{ label: 'Read the quickstart', href: 'https://github.com/parthadrizz/drizz-farm#quickstart' }}
          />
        </div>
        {/* Slide-out create panel (mirrors the standard layout below) */}
        <div className={`fixed inset-y-0 right-0 z-50 transition-transform duration-300 ease-in-out ${showCreateModal ? 'translate-x-0' : 'translate-x-full'}`}
          style={{ width: 'min(560px, 90vw)' }}>
          {showCreateModal && (
            <div className="fixed inset-0 -z-10" style={{ background: 'hsl(var(--surface-0) / 0.6)' }}
              onClick={() => setShowCreateModal(false)} />
          )}
          <div className="h-full surface-1 border-l border-border flex flex-col shadow-2xl">
            <div className="flex-1 overflow-y-auto p-6">
              {showCreateModal && <CreateWizard onClose={() => { setShowCreateModal(false); refresh(); }} />}
            </div>
          </div>
        </div>
      </div>
    );
  }

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
                <h1 className="text-sm font-semibold text-foreground">
                  {groupName || selfName || 'drizz-farm'}
                </h1>
              </div>
              {selfHealth && (
                <div className="text-[11px] text-muted-foreground font-mono mt-0.5">
                  v{selfHealth.version} · {selfHealth.resources.num_cpu} CPUs · {formatUptime(selfHealth.uptime)}
                </div>
              )}
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

      {/* Per-node sections */}
      {nodes.map(node => (
        <NodeSection
          key={node.entry.name}
          node={node}
          isSelf={node.entry.name === selfName}
          actionLoading={actionLoading}
          onBoot={(avdName) => handleBoot(node, avdName)}
          onShutdown={(inst) => handleShutdown(node, inst)}
          onView={(id) => navigate(`/live/${id}`)}
          onAdd={() => setShowCreateModal(true)}
        />
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
            {showCreateModal && <CreateWizard onClose={() => { setShowCreateModal(false); refresh(); }} />}
          </div>
        </div>
      </div>
    </div>
  );
}

function NodeSection({
  node,
  isSelf,
  actionLoading,
  onBoot,
  onShutdown,
  onView,
  onAdd,
}: {
  node: NodeSnapshot;
  isSelf: boolean;
  actionLoading: string | null;
  onBoot: (avdName: string) => void;
  onShutdown: (inst: DeviceInstance) => void;
  onView: (id: string) => void;
  onAdd: () => void;
}) {
  if (!node.online) {
    return (
      <div className="section-card p-4">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-2 min-w-0">
            <Server className="w-3.5 h-3.5 text-destructive/70 flex-shrink-0" />
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <span className="text-sm text-foreground">{node.entry.name}</span>
                <span className="badge badge-error">OFFLINE</span>
                {isSelf && <span className="badge badge-node">THIS NODE</span>}
              </div>
              <div className="text-[11px] font-mono text-muted-foreground truncate">
                {node.entry.url}
              </div>
              <div className="text-[11px] text-muted-foreground mt-0.5">
                Not reachable on the network — is this Mac powered on, awake, and on the same LAN?
                {node.error && <> · <span className="font-mono opacity-70">{node.error}</span></>}
              </div>
            </div>
          </div>
          <div className="flex items-center gap-2 flex-shrink-0">
            <a
              href={node.entry.url}
              target="_blank"
              rel="noopener noreferrer"
              className="action-btn surface-3 text-foreground text-xs"
            >
              Open
            </a>
            <a
              href="/nodes"
              className="action-btn surface-3 text-muted-foreground hover:text-foreground text-xs"
            >
              Manage
            </a>
          </div>
        </div>
      </div>
    );
  }

  const pool = node.pool!;
  const avds = node.avds || [];
  const poolByName = new Map<string, DeviceInstance>();
  pool.instances.forEach(inst => poolByName.set(inst.device_name, inst));
  const avdSet = new Set(avds.map(a => a.name));

  const rows = [
    ...avds.map(a => ({ name: a.name, displayName: a.display_name || '', inst: poolByName.get(a.name), state: poolByName.get(a.name)?.state || 'offline' })),
    ...pool.instances.filter(i => !avdSet.has(i.device_name) && i.device_kind !== 'android_usb').map(inst => ({ name: inst.device_name, displayName: inst.display_info, inst, state: inst.state })),
    ...pool.instances.filter(i => i.device_kind === 'android_usb').map(inst => ({ name: inst.device_name, displayName: inst.display_info, inst, state: inst.state })),
  ];

  const usbCount = pool.instances.filter(i => i.device_kind === 'android_usb').length;

  return (
    <div className="section-card">
      <div className="section-header">
        <div className="flex items-center gap-2.5">
          <Server className="w-3.5 h-3.5 text-muted-foreground" />
          <h2 className="text-[13px] font-semibold text-foreground">{node.entry.name}</h2>
          {isSelf && <span className="badge badge-node">THIS NODE</span>}
        </div>
        <div className="flex items-center gap-3">
          <span className="text-[11px] text-muted-foreground font-mono">
            {avds.length} AVDs · {usbCount} USB
          </span>
          {isSelf && (
            <button onClick={onAdd} className="action-btn flex items-center gap-1.5 bg-[hsl(var(--primary))] text-primary-foreground hover:bg-[hsl(var(--primary))]/90 shadow-sm" style={{ boxShadow: '0 0 12px -2px hsl(var(--primary) / 0.3)' }}>
              <Plus className="w-3 h-3" /> Add
            </button>
          )}
        </div>
      </div>

      <div className="divide-y divide-border/50">
        {rows.length === 0 ? (
          <EmptyState
            compact
            icon={Smartphone}
            title="No devices on this node yet"
            description={
              isSelf
                ? 'Create an AVD or plug in a USB phone to get started.'
                : 'This node has no AVDs configured yet.'
            }
            primary={isSelf ? { label: 'Add device', icon: Plus, onClick: onAdd } : undefined}
          />
        ) : rows.map(({ name, displayName, inst, state }) => {
          const reserved = !!inst?.reserved;
          const kebabItems: KebabItem[] = inst
            ? [
                reserved
                  ? {
                      label: 'Unreserve',
                      icon: Unlock,
                      onClick: async () => {
                        try {
                          await api.peer.unreserve(node.entry.url, inst.id);
                        } catch (e: any) { alert(e?.message || 'unreserve failed'); }
                      },
                    }
                  : {
                      label: 'Reserve for manual',
                      icon: Lock,
                      onClick: async () => {
                        const label = prompt('Optional label (who/why is this reserved?)') || '';
                        try {
                          await api.peer.reserve(node.entry.url, inst.id, label);
                        } catch (e: any) { alert(e?.message || 'reserve failed'); }
                      },
                    },
              ]
            : [];
          return (
            <div key={name} className="px-5 py-3 flex items-center gap-4 card-hover group animate-slide-in">
              <div className="flex-1 min-w-0">
                <div className="text-[13px] font-medium text-foreground truncate font-mono flex items-center gap-2">
                  {name}
                  {reserved && (
                    <span
                      className="badge"
                      title={inst?.reserved_label || 'Reserved for manual use — automated callers skip this device'}
                      style={{ background: 'hsl(45 90% 60% / 0.12)', color: 'hsl(45 95% 65%)', border: '1px solid hsl(45 90% 60% / 0.25)' }}
                    >
                      RESERVED{inst?.reserved_label ? ` · ${inst.reserved_label}` : ''}
                    </span>
                  )}
                </div>
                {displayName && <div className="text-[11px] text-purple-400 truncate mt-0.5">{displayName}</div>}
                {inst && (
                  <div className="text-[10px] text-muted-foreground/60 font-mono mt-0.5">
                    {inst.serial}:{inst.connection?.adb_port || '-'}
                  </div>
                )}
              </div>
              <StatusBadge state={state} />
              <div className="flex items-center gap-1.5 opacity-80 group-hover:opacity-100 transition-opacity">
                {(state === 'warm' || state === 'allocated') && inst && (
                  <ActionButton onClick={() => onView(inst.id)} variant="accent">View</ActionButton>
                )}
                {state === 'offline' && (
                  <ActionButton onClick={() => onBoot(name)} loading={actionLoading === name} variant="primary">Start</ActionButton>
                )}
                {state === 'warm' && inst && (
                  <ActionButton onClick={() => onShutdown(inst)} loading={actionLoading === inst.id} variant="danger">Stop</ActionButton>
                )}
                {kebabItems.length > 0 && <Kebab items={kebabItems} />}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
