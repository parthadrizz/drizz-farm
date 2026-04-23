/**
 * Nodes — first-class page in the sidebar that shows every node in
 * the current group. Each row shows live online/offline status (we
 * ping the peer's /api/v1/health from the browser, with a 2s timeout)
 * and clicking a peer opens that node's own dashboard in a new tab.
 *
 * This implements the registry-model spec: each node serves its own
 * dashboard, and the browser routes between them. No backend-to-
 * backend proxying.
 */
import { useEffect, useState } from 'react';
import { Server, ExternalLink, Eye, EyeOff, Copy, Check, Wifi, RefreshCw, Plus } from 'lucide-react';
import { Link } from 'react-router-dom';
import { api, GroupInfo, NodeList, NodeEntry } from '../lib/api';

type Health = 'online' | 'offline' | 'checking';

async function probe(url: string, signal: AbortSignal): Promise<Health> {
  try {
    const u = url.replace(/\/$/, '') + '/api/v1/node/health';
    const r = await fetch(u, { signal, mode: 'cors' });
    return r.ok ? 'online' : 'offline';
  } catch {
    return 'offline';
  }
}

export function Nodes() {
  const [group, setGroup] = useState<GroupInfo | null>(null);
  const [nodes, setNodes] = useState<NodeList | null>(null);
  const [healths, setHealths] = useState<Record<string, Health>>({});
  const [showKey, setShowKey] = useState(false);
  const [copied, setCopied] = useState(false);
  const [refreshing, setRefreshing] = useState(false);

  const load = async () => {
    setRefreshing(true);
    try {
      const [g, l] = await Promise.all([api.groupInfo(), api.listNodes()]);
      setGroup(g);
      setNodes(l);
    } catch (e) {
      console.error('nodes: load failed', e);
    } finally {
      setRefreshing(false);
    }
  };

  useEffect(() => {
    load();
    const i = setInterval(load, 10000);
    return () => clearInterval(i);
  }, []);

  // Probe every node's health whenever the node list refreshes.
  useEffect(() => {
    if (!nodes) return;
    const ctrl = new AbortController();
    const timeout = setTimeout(() => ctrl.abort(), 2500);
    nodes.nodes.forEach(async (n) => {
      setHealths((h) => ({ ...h, [n.name]: 'checking' }));
      const h = await probe(n.url, ctrl.signal);
      setHealths((prev) => ({ ...prev, [n.name]: h }));
    });
    return () => {
      clearTimeout(timeout);
      ctrl.abort();
    };
  }, [nodes]);

  const copy = (text: string) => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  // Standalone — no group yet. Send the user to Settings to create/join.
  if (group && !group.has_group) {
    return (
      <div className="space-y-6">
        <header className="flex items-center justify-between">
          <div>
            <h1 className="text-xl font-semibold tracking-tight">Nodes</h1>
            <p className="text-sm text-muted-foreground mt-0.5">
              Standalone — not in any group yet.
            </p>
          </div>
        </header>
        <div className="section-card">
          <div className="p-8 text-center space-y-3">
            <Server className="w-8 h-8 text-muted-foreground mx-auto" />
            <p className="text-sm text-foreground">No group configured.</p>
            <p className="text-xs text-muted-foreground">
              Create one to get a key your teammates can use to add their Macs.
            </p>
            <Link
              to="/settings"
              className="action-btn action-btn-primary inline-flex items-center gap-1.5 mt-2"
            >
              <Plus className="w-3.5 h-3.5" /> Create or join a group
            </Link>
          </div>
        </div>
      </div>
    );
  }

  const key = group?.group_key || '';
  const masked = key ? '•'.repeat(Math.min(key.length, 32)) : '';

  return (
    <div className="space-y-6">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Nodes</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            {group?.group_name && (
              <>
                Group <span className="font-mono text-foreground">{group.group_name}</span> ·{' '}
                {nodes?.nodes.length ?? 0} node{(nodes?.nodes.length ?? 0) !== 1 ? 's' : ''}
              </>
            )}
          </p>
        </div>
        <button
          onClick={load}
          className="action-btn surface-3 text-foreground inline-flex items-center gap-1.5"
        >
          <RefreshCw className={`w-3.5 h-3.5 ${refreshing ? 'animate-spin' : ''}`} />
          Refresh
        </button>
      </header>

      {key && (
        <div className="section-card">
          <div className="section-header">
            <div className="flex items-center gap-2">
              <Wifi className="w-3.5 h-3.5 text-primary" />
              <span className="text-sm font-semibold text-foreground">Group key</span>
            </div>
          </div>
          <div className="p-5 space-y-2">
            <div className="flex items-center gap-2 surface-0 rounded-lg px-4 py-2.5">
              <code className="flex-1 text-sm font-mono text-foreground select-all truncate">
                {showKey ? key : masked}
              </code>
              <button
                onClick={() => setShowKey((v) => !v)}
                className="p-1 rounded hover:bg-surface-2 text-muted-foreground hover:text-foreground"
                title={showKey ? 'Hide key' : 'Show key'}
              >
                {showKey ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
              </button>
              <button
                onClick={() => copy(key)}
                className="p-1 rounded hover:bg-surface-2 text-muted-foreground hover:text-foreground"
                title="Copy key"
              >
                {copied ? <Check className="w-3.5 h-3.5 text-primary" /> : <Copy className="w-3.5 h-3.5" />}
              </button>
            </div>
            {group?.self?.url && (
              <p className="text-[11px] text-muted-foreground">
                Have a teammate run:{' '}
                <code className="font-mono text-foreground">
                  drizz-farm join {group.self.url} &lt;key&gt;
                </code>
              </p>
            )}
          </div>
        </div>
      )}

      <div className="section-card">
        <div className="divide-y divide-border/50">
          {nodes?.nodes.map((n) => (
            <NodeRow
              key={n.name}
              node={n}
              isSelf={n.name === group?.self?.name}
              health={healths[n.name] ?? 'checking'}
            />
          ))}
          {(!nodes || nodes.nodes.length === 0) && (
            <div className="p-8 text-center text-sm text-muted-foreground">
              No nodes registered yet.
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function NodeRow({
  node,
  isSelf,
  health,
}: {
  node: NodeEntry;
  isSelf: boolean;
  health: Health;
}) {
  const dotClass =
    health === 'online'
      ? 'bg-primary'
      : health === 'offline'
      ? 'bg-destructive'
      : 'bg-muted-foreground/40';
  const label =
    health === 'online' ? 'online' : health === 'offline' ? 'offline' : 'checking…';

  // For self, route in-app. For peers, open the peer's own dashboard
  // in a new tab — that's the registry model: each node owns its UI.
  const Wrapper: React.FC<{ children: React.ReactNode }> = ({ children }) =>
    isSelf ? (
      <Link to="/" className="block">
        {children}
      </Link>
    ) : (
      <a
        href={node.url}
        target="_blank"
        rel="noopener noreferrer"
        className="block"
      >
        {children}
      </a>
    );

  return (
    <Wrapper>
      <div className="px-5 py-4 flex items-center justify-between hover:surface-2 transition-colors">
        <div className="flex items-center gap-3">
          <Server className="w-3.5 h-3.5 text-muted-foreground" />
          <div>
            <div className="text-sm font-medium text-foreground flex items-center gap-2">
              {node.name}
              {isSelf && <span className="badge badge-node">THIS NODE</span>}
            </div>
            <div className="text-[11px] font-mono text-muted-foreground">{node.url}</div>
          </div>
        </div>
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <span className={`w-1.5 h-1.5 rounded-full ${dotClass}`} />
            {label}
          </div>
          {!isSelf && <ExternalLink className="w-3.5 h-3.5 text-muted-foreground" />}
        </div>
      </div>
    </Wrapper>
  );
}
