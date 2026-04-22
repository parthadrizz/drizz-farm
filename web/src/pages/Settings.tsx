import { useEffect, useState } from 'react';
import { api, GroupInfo, NodeList, CreateGroupResult } from '../lib/api';
import { Wifi, Copy, Check, Server, Trash2 } from 'lucide-react';

type Section = 'pool' | 'cleanup' | 'health' | 'network' | 'artifacts' | 'node' | 'api' | 'group' | 'raw';

export function Settings() {
  const [cfg, setCfg] = useState<any>(null);
  const [configYaml, setConfigYaml] = useState('');
  const [activeSection, setActiveSection] = useState<Section>('pool');
  const [editBuffer, setEditBuffer] = useState('');
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');

  // Group / registry state
  const [group, setGroup] = useState<GroupInfo | null>(null);
  const [nodes, setNodes] = useState<NodeList | null>(null);
  const [newGroupKey, setNewGroupKey] = useState<string>(''); // shown once after create

  const refreshGroup = async () => {
    try {
      const [g, l] = await Promise.all([api.groupInfo(), api.listNodes()]);
      setGroup(g);
      setNodes(l);
    } catch { /* ignore — new backend may not be live yet */ }
  };

  useEffect(() => {
    (async () => {
      try {
        const [raw, parsed] = await Promise.all([api.getConfigRaw(), api.getConfig()]);
        setConfigYaml(raw); setCfg(parsed); setEditBuffer(raw); setError('');
      } catch (e: any) { setError(e.message); }
    })();
    refreshGroup();
    const i = setInterval(refreshGroup, 5000);
    return () => clearInterval(i);
  }, []);

  const handleSaveRaw = async () => {
    setSaving(true);
    try {
      const result = await api.saveConfigRaw(editBuffer);
      setMessage(result.message || 'Saved'); setConfigYaml(editBuffer);
      setTimeout(() => setMessage(''), 5000);
    } catch (e: any) { setError(e.message); }
    setSaving(false);
  };

  if (error) return <div className="text-center py-20 text-destructive animate-fade-in">{error}</div>;
  if (!cfg) return (
    <div className="flex items-center justify-center py-20">
      <div className="w-5 h-5 border-2 border-muted border-t-primary rounded-full animate-spin" />
      <span className="ml-3 text-muted-foreground text-sm">Loading...</span>
    </div>
  );

  const sections: { id: Section; label: string }[] = [
    { id: 'pool', label: 'Pool' }, { id: 'cleanup', label: 'Cleanup' },
    { id: 'health', label: 'Health' }, { id: 'network', label: 'Network' },
    { id: 'artifacts', label: 'Artifacts' }, { id: 'node', label: 'Node' },
    { id: 'api', label: 'API' }, { id: 'group', label: 'Group' }, { id: 'raw', label: 'Raw YAML' },
  ];

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-bold text-foreground">Settings</h1>
        {message && <span className="text-sm text-primary font-medium">{message}</span>}
      </div>

      <div className="flex gap-1 border-b border-border pb-px overflow-x-auto">
        {sections.map(s => (
          <button key={s.id} onClick={() => { setActiveSection(s.id); if (s.id === 'raw') setEditBuffer(configYaml); }}
            className={`tab-btn ${activeSection === s.id ? 'tab-btn-active' : 'tab-btn-inactive'}`}>
            {s.label}
          </button>
        ))}
      </div>

      {activeSection === 'pool' && (
        <FormSection title="Pool Configuration" description="Controls how many emulators can run and session behavior.">
          <Field label="Max Concurrent" value={cfg.Pool?.MaxConcurrent} hint="Maximum emulators running at once." />
          <Field label="Session Timeout" value={cfg.Pool?.SessionTimeoutMinutes} unit="minutes" hint="Default session duration." />
          <Field label="Max Session Duration" value={cfg.Pool?.SessionMaxMinutes} unit="minutes" hint="Hard cap on session length." />
          <Field label="Queue Size" value={cfg.Pool?.QueueMaxSize} hint="Max pending requests." />
          <Field label="Queue Timeout" value={cfg.Pool?.QueueTimeoutSeconds} unit="seconds" hint="How long requests wait." />
          <Field label="Port Range" value={`${cfg.Pool?.PortRangeMin} – ${cfg.Pool?.PortRangeMax}`} hint="ADB port range." />
        </FormSection>
      )}

      {activeSection === 'cleanup' && (
        <FormSection title="Cleanup & Idle" description="When and how devices are shut down.">
          <Field label="Idle Timeout" value={cfg.Cleanup?.IdleTimeoutMinutes || 5} unit="minutes" hint="Shut down warm emulators after idle." />
          <Field label="On Session End" value={cfg.Cleanup?.OnSessionEnd} hint="Reset strategy." />
          <Field label="Orphan Check Interval" value={cfg.Cleanup?.OrphanCheckIntervalSecs} unit="seconds" />
          <Field label="Disk Cleanup Threshold" value={cfg.Cleanup?.DiskCleanupThresholdGB} unit="GB" />
        </FormSection>
      )}

      {activeSection === 'health' && (
        <FormSection title="Health Checks" description="How devices are monitored for failures.">
          <Field label="Check Interval" value={cfg.HealthCheck?.IntervalSeconds} unit="seconds" />
          <Field label="Unhealthy Threshold" value={cfg.HealthCheck?.UnhealthyThreshold} hint="Consecutive failures before error." />
        </FormSection>
      )}

      {activeSection === 'network' && (
        <FormSection title="Network" description="How other browsers reach this node.">
          <Field label="External URL" value={cfg.Node?.ExternalURL || '(auto: hostname.local)'} hint="Override for Tailscale, ngrok, or hub deployment." />
          <Field label="Allowed CIDRs" value={(cfg.Network?.AllowedCIDRs || []).join(', ')} />
        </FormSection>
      )}

      {activeSection === 'artifacts' && (
        <FormSection title="Artifacts" description="What gets captured during sessions.">
          <Field label="Video Recording" value={cfg.Artifacts?.VideoRecording ? 'true' : 'false'} />
          <Field label="Screenshot on Failure" value={cfg.Artifacts?.ScreenshotOnFail ? 'true' : 'false'} />
          <Field label="Logcat Capture" value={cfg.Artifacts?.LogcatCapture ? 'true' : 'false'} />
          <Field label="Network HAR" value={cfg.Artifacts?.NetworkHARCapture ? 'true' : 'false'} />
          <Field label="Retention" value={cfg.Artifacts?.RetentionDays} unit="days" />
        </FormSection>
      )}

      {activeSection === 'node' && (
        <FormSection title="Node" description="This machine's identity and logging.">
          <Field label="Node Name" value={cfg.Node?.Name} hint="Empty = auto-detected." />
          <Field label="External URL" value={cfg.Node?.ExternalURL} hint="Empty = http://hostname.local:port" />
          <Field label="Log Level" value={cfg.Node?.LogLevel} />
          <Field label="Data Directory" value={cfg.Node?.DataDir} />
          <Field label="Metrics Port" value={cfg.Node?.MetricsPort} />
        </FormSection>
      )}

      {activeSection === 'api' && (
        <FormSection title="API Server" description="HTTP and gRPC endpoints.">
          <Field label="Host" value={cfg.API?.Host} hint="Bind address." />
          <Field label="REST Port" value={cfg.API?.Port} />
          <Field label="gRPC Port" value={cfg.API?.GRPCPort} />
        </FormSection>
      )}

      {activeSection === 'group' && (
        <GroupSection
          group={group}
          nodes={nodes}
          newGroupKey={newGroupKey}
          onCreate={async (name: string) => {
            try {
              const res: CreateGroupResult = await api.createGroup(name);
              setNewGroupKey(res.group_key);
              await refreshGroup();
              setMessage('Group created — copy the key now, it won’t be shown again');
              setTimeout(() => setMessage(''), 8000);
            } catch (e: any) { setError(e.message); }
          }}
          onJoin={async (peerURL: string, key: string) => {
            try {
              await api.joinGroup(peerURL, key);
              await refreshGroup();
              setMessage('Joined group');
              setTimeout(() => setMessage(''), 5000);
            } catch (e: any) {
              setError(e.message);
              throw e;
            }
          }}
          onLeave={async () => {
            try {
              await api.leaveGroup();
              setNewGroupKey('');
              await refreshGroup();
              setMessage('Left group — node is now standalone');
              setTimeout(() => setMessage(''), 5000);
            } catch (e: any) { setError(e.message); }
          }}
          onDismissKey={() => setNewGroupKey('')}
        />
      )}

      {activeSection === 'raw' && (
        <div className="space-y-4">
          <div className="section-card overflow-hidden">
            <div className="section-header">
              <span className="text-xs text-muted-foreground font-mono">~/.drizz-farm/config.yaml</span>
              <span className="text-xs text-muted-foreground/50">{editBuffer.split('\n').length} lines</span>
            </div>
            <textarea value={editBuffer} onChange={e => setEditBuffer(e.target.value)}
              className="w-full h-[500px] surface-0 p-4 text-sm font-mono text-foreground focus:outline-none resize-none leading-6 border-none"
              spellCheck={false} />
          </div>
          <div className="flex items-center justify-between">
            <p className="text-xs text-muted-foreground">Restart daemon after saving.</p>
            <div className="flex gap-3">
              <button onClick={() => setEditBuffer(configYaml)} className="action-btn surface-3 text-foreground">Reset</button>
              <button onClick={handleSaveRaw} disabled={saving || editBuffer === configYaml}
                className="action-btn action-btn-primary disabled:opacity-40">
                {saving ? 'Saving...' : 'Save'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function FormSection({ title, description, children }: { title: string; description: string; children: React.ReactNode }) {
  return (
    <div className="section-card">
      <div className="px-5 py-4 border-b border-border">
        <h2 className="text-sm font-semibold text-foreground">{title}</h2>
        <p className="text-xs text-muted-foreground mt-0.5">{description}</p>
      </div>
      <div className="divide-y divide-border/50">{children}</div>
    </div>
  );
}

function Field({ label, value, unit, hint }: { label: string; value: any; unit?: string; hint?: string }) {
  const display = value !== undefined && value !== null && value !== '' ? String(value) : '—';
  return (
    <div className="px-5 py-3.5 flex items-center justify-between">
      <div>
        <div className="text-sm text-foreground">{label}</div>
        {hint && <div className="text-[11px] text-muted-foreground mt-0.5">{hint}</div>}
      </div>
      <div className="text-sm font-mono text-foreground/80">
        {display}{unit ? <span className="text-muted-foreground ml-1">{unit}</span> : ''}
      </div>
    </div>
  );
}

// ── Group section ─────────────────────────────────────────────────────────
// Each node is independent. A "group" is just a shared list of node URLs + an
// auth key for modifying that list. No leader, no sync, no consensus.
function GroupSection({
  group, nodes, newGroupKey,
  onCreate, onJoin, onLeave, onDismissKey,
}: {
  group: GroupInfo | null;
  nodes: NodeList | null;
  newGroupKey: string;
  onCreate: (name: string) => void;
  onJoin: (peerURL: string, key: string) => Promise<void>;
  onLeave: () => void;
  onDismissKey: () => void;
}) {
  const [name, setName] = useState('');
  const [copied, setCopied] = useState(false);
  const [mode, setMode] = useState<'choose' | 'create' | 'join'>('choose');
  const [joinURL, setJoinURL] = useState('');
  const [joinKey, setJoinKey] = useState('');
  const [joining, setJoining] = useState(false);
  const [joinError, setJoinError] = useState('');

  const copy = (text: string) => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  // Just-created group: show the key once with clear instructions.
  if (newGroupKey) {
    return (
      <div className="section-card">
        <div className="section-header">
          <div className="flex items-center gap-2">
            <Wifi className="w-3.5 h-3.5 text-primary" />
            <span className="text-sm font-semibold text-foreground">Group created</span>
          </div>
        </div>
        <div className="p-5 space-y-4">
          <div className="text-xs text-muted-foreground">
            Copy this key now. It won’t be shown again. Share it with other nodes so they can join the group.
          </div>
          <div className="flex items-center gap-2 surface-0 rounded-lg px-4 py-3">
            <code className="flex-1 text-sm font-mono text-primary select-all">{newGroupKey}</code>
            <button onClick={() => copy(newGroupKey)} className="p-1 rounded hover:bg-surface-2 text-muted-foreground hover:text-foreground">
              {copied ? <Check className="w-3.5 h-3.5 text-primary" /> : <Copy className="w-3.5 h-3.5" />}
            </button>
          </div>
          <button onClick={onDismissKey} className="action-btn action-btn-primary">I saved the key</button>
        </div>
      </div>
    );
  }

  // Active group view — list members.
  if (group?.has_group && nodes) {
    return (
      <div className="space-y-4">
        <div className="section-card">
          <div className="section-header">
            <div className="flex items-center gap-2">
              <Wifi className="w-3.5 h-3.5 text-primary" />
              <span className="text-sm font-semibold text-foreground">{group.group_name}</span>
            </div>
            <span className="text-xs text-muted-foreground">{nodes.nodes.length} node{nodes.nodes.length !== 1 ? 's' : ''}</span>
          </div>
          <div className="divide-y divide-border/50">
            {nodes.nodes.map(n => (
              <div key={n.name} className="px-5 py-3.5 flex items-center justify-between">
                <div className="flex items-center gap-3">
                  <Server className="w-3.5 h-3.5 text-muted-foreground" />
                  <div>
                    <div className="text-sm font-medium text-foreground">
                      {n.name}
                      {n.name === group.self.name && <span className="ml-2 badge badge-node">THIS NODE</span>}
                    </div>
                    <div className="text-[11px] font-mono text-muted-foreground">{n.url}</div>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
        <div className="flex justify-end">
          <button onClick={onLeave} className="action-btn action-btn-danger flex items-center gap-1.5">
            <Trash2 className="w-3 h-3" /> Leave group
          </button>
        </div>
      </div>
    );
  }

  // Create form
  if (mode === 'create') {
    return (
      <div className="section-card">
        <div className="px-5 py-4 border-b border-border">
          <h2 className="text-sm font-semibold text-foreground">Create group</h2>
          <p className="text-xs text-muted-foreground mt-0.5">Name your group. You'll get a key to share with other nodes.</p>
        </div>
        <div className="p-5 space-y-4">
          <div>
            <label className="text-xs text-muted-foreground block mb-1.5">Group name</label>
            <input type="text" value={name} onChange={e => setName(e.target.value)} placeholder="e.g. my-lab"
              className="w-full surface-2 border border-border rounded-lg px-3 py-2 text-sm text-foreground" />
          </div>
          <div className="flex items-center justify-end gap-3">
            <button onClick={() => { setMode('choose'); setName(''); }} className="action-btn surface-3 text-foreground">Cancel</button>
            <button onClick={() => name.trim() && onCreate(name.trim())} disabled={!name.trim()}
              className="action-btn action-btn-primary disabled:opacity-40">Create</button>
          </div>
        </div>
      </div>
    );
  }

  // Join form
  if (mode === 'join') {
    return (
      <div className="section-card">
        <div className="px-5 py-4 border-b border-border">
          <h2 className="text-sm font-semibold text-foreground">Join group</h2>
          <p className="text-xs text-muted-foreground mt-0.5">Paste the URL of any node already in the group, plus the group key.</p>
        </div>
        <div className="p-5 space-y-4">
          <div>
            <label className="text-xs text-muted-foreground block mb-1.5">Peer URL</label>
            <input type="text" value={joinURL} onChange={e => setJoinURL(e.target.value)}
              placeholder="http://mac-mini-1.local:9401"
              className="w-full surface-2 border border-border rounded-lg px-3 py-2 text-sm font-mono text-foreground" />
          </div>
          <div>
            <label className="text-xs text-muted-foreground block mb-1.5">Group key</label>
            <input type="text" value={joinKey} onChange={e => setJoinKey(e.target.value)}
              placeholder="paste key shared by another node"
              className="w-full surface-2 border border-border rounded-lg px-3 py-2 text-sm font-mono text-foreground" />
          </div>
          {joinError && <p className="text-sm text-destructive">{joinError}</p>}
          <div className="flex items-center justify-end gap-3">
            <button onClick={() => { setMode('choose'); setJoinURL(''); setJoinKey(''); setJoinError(''); }}
              className="action-btn surface-3 text-foreground">Cancel</button>
            <button
              disabled={joining || !joinURL.trim() || !joinKey.trim()}
              onClick={async () => {
                setJoining(true); setJoinError('');
                try {
                  await onJoin(joinURL.trim(), joinKey.trim());
                  setMode('choose'); setJoinURL(''); setJoinKey('');
                } catch (e: any) {
                  setJoinError(e?.message || 'join failed');
                }
                setJoining(false);
              }}
              className="action-btn action-btn-primary disabled:opacity-40">
              {joining ? 'Joining…' : 'Join'}
            </button>
          </div>
        </div>
      </div>
    );
  }

  // Standalone — choose
  return (
    <div className="section-card">
      <div className="p-8 text-center space-y-4">
        <Wifi className="w-8 h-8 text-muted-foreground mx-auto" />
        <div>
          <h3 className="text-sm font-semibold text-foreground">Standalone</h3>
          <p className="text-xs text-muted-foreground mt-1 max-w-md mx-auto">
            This node isn't part of any group. Create a new group, or join an existing one.
          </p>
        </div>
        <div className="flex items-center justify-center gap-2">
          <button onClick={() => setMode('create')} className="action-btn action-btn-primary">Create group</button>
          <button onClick={() => setMode('join')} className="action-btn surface-3 text-foreground">Join group</button>
        </div>
      </div>
    </div>
  );
}
