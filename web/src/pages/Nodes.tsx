import { useEffect, useState } from 'react';
import { Server, Wifi, Cpu, HardDrive, Clock } from 'lucide-react';
import { api, NodeHealth } from '../lib/api';
import { StatusDot } from '../components/StatusBadge';

interface NodeInfo {
  name: string;
  host: string;
  role: string;
  capacity?: number;
  warm?: number;
  allocated?: number;
  available?: number;
  num_cpu?: number;
  memory_mb?: number;
  score?: number;
  healthy: boolean;
  last_seen?: string;
}

interface FederatedStatus {
  leader: string;
  nodes: NodeInfo[];
  total_nodes: number;
  total_capacity: number;
  total_allocated: number;
  total_available: number;
}

export function Nodes() {
  const [health, setHealth] = useState<NodeHealth | null>(null);
  const [federation, setFederation] = useState<FederatedStatus | null>(null);
  const [error, setError] = useState('');

  const refresh = async () => {
    try {
      const [h, fed] = await Promise.all([
        api.health(),
        api.federationStatus().catch(() => null),
      ]);
      setHealth(h);
      setFederation(fed);
      setError('');
    } catch (e: any) { setError(e.message); }
  };

  useEffect(() => { refresh(); const i = setInterval(refresh, 5000); return () => clearInterval(i); }, []);

  if (error) return (
    <div className="text-center py-20 text-destructive animate-fade-in">{error}</div>
  );

  if (!health || !federation) return (
    <div className="flex items-center justify-center py-20">
      <div className="w-5 h-5 border-2 border-muted border-t-primary rounded-full animate-spin" />
      <span className="ml-3 text-muted-foreground text-sm">Loading...</span>
    </div>
  );

  const totalCap = federation.total_capacity;
  const totalAlloc = federation.total_allocated;
  const totalAvail = federation.total_available;

  return (
    <div className="space-y-5 animate-fade-in">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-bold text-foreground">Nodes</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            {federation.total_nodes} node{federation.total_nodes > 1 ? 's' : ''} in mesh
            {health.mesh?.name ? ` "${health.mesh.name}"` : ''}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Wifi className="w-4 h-4 text-primary" />
          <span className="text-sm font-mono text-muted-foreground">
            {totalCap} capacity · {totalAvail} available · {totalAlloc} in use
          </span>
        </div>
      </div>

      {/* Mesh info */}
      <div className="section-card">
        <div className="section-header">
          <div className="flex items-center gap-2">
            <Wifi className="w-3.5 h-3.5 text-primary" />
            <span className="text-sm font-semibold text-foreground">Mesh Overview</span>
          </div>
          <span className="text-xs text-muted-foreground font-mono">leader: {federation.leader}</span>
        </div>
        <div className="grid grid-cols-4 divide-x divide-border">
          <div className="stat-card">
            <div className="stat-label">Nodes</div>
            <div className="stat-value text-foreground">{federation.total_nodes}</div>
          </div>
          <div className="stat-card">
            <div className="stat-label">Capacity</div>
            <div className="stat-value text-foreground">{totalCap}</div>
          </div>
          <div className="stat-card">
            <div className="stat-label">Available</div>
            <div className="stat-value text-primary">{totalAvail}</div>
          </div>
          <div className="stat-card">
            <div className="stat-label">In Use</div>
            <div className="stat-value text-accent">{totalAlloc}</div>
          </div>
        </div>
      </div>

      {/* Node list */}
      <div className="section-card divide-y divide-border/50">
        {federation.nodes.map(node => (
          <div key={node.host} className="px-5 py-4 card-hover">
            <div className="flex items-start justify-between">
              <div className="flex items-start gap-3">
                <div className="mt-0.5">
                  <StatusDot state={node.healthy ? 'running' : 'error'} />
                </div>
                <div>
                  <div className="flex items-center gap-2">
                    <Server className="w-3.5 h-3.5 text-muted-foreground" />
                    <span className="text-sm font-semibold text-foreground">{node.name || node.host}</span>
                    <span className={`badge ${node.role === 'leader' ? 'badge-leader' : 'badge-node'}`}>
                      {node.role}
                    </span>
                  </div>
                  <div className="text-xs text-muted-foreground font-mono mt-1">{node.host}</div>

                  {/* Stats row */}
                  <div className="flex items-center gap-4 mt-2 text-xs text-muted-foreground">
                    <span className="flex items-center gap-1">
                      <HardDrive className="w-3 h-3" />
                      {node.capacity || 0} capacity
                    </span>
                    <span className="text-primary">{node.available || 0} available</span>
                    <span className="text-accent">{node.allocated || 0} allocated</span>
                    {node.num_cpu && (
                      <span className="flex items-center gap-1">
                        <Cpu className="w-3 h-3" />
                        {node.num_cpu} CPUs
                      </span>
                    )}
                    {node.memory_mb && (
                      <span>{Math.round(node.memory_mb / 1024)}GB RAM</span>
                    )}
                  </div>
                </div>
              </div>

              <div className="text-right">
                {node.score !== undefined && (
                  <div className="text-xs font-mono text-muted-foreground/60">score: {node.score}</div>
                )}
                {!node.healthy && (
                  <div className="text-xs text-destructive mt-1">unreachable</div>
                )}
              </div>
            </div>
          </div>
        ))}
      </div>

      {/* Mesh key info */}
      <div className="section-card p-4">
        <div className="flex items-center justify-between">
          <div>
            <div className="text-xs text-muted-foreground">Mesh Key</div>
            <div className="text-xs font-mono text-foreground/60 mt-0.5">Share this key with other machines to join this mesh</div>
          </div>
          <button onClick={() => {
            // Fetch config to get mesh key
            api.getConfig().then((cfg: any) => {
              const key = cfg.Mesh?.Key || cfg.mesh?.key || '';
              navigator.clipboard.writeText(key);
              alert('Mesh key copied to clipboard');
            });
          }} className="action-btn action-btn-primary">
            Copy Key
          </button>
        </div>
      </div>
    </div>
  );
}
