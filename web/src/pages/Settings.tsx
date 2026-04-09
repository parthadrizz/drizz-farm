import { useEffect, useState } from 'react';
import { api } from '../lib/api';

type Section = 'pool' | 'cleanup' | 'health' | 'network' | 'artifacts' | 'node' | 'api' | 'raw';

export function Settings() {
  const [cfg, setCfg] = useState<any>(null);
  const [configYaml, setConfigYaml] = useState('');
  const [activeSection, setActiveSection] = useState<Section>('pool');
  const [editBuffer, setEditBuffer] = useState('');
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');

  useEffect(() => {
    (async () => {
      try {
        const [raw, parsed] = await Promise.all([api.getConfigRaw(), api.getConfig()]);
        setConfigYaml(raw); setCfg(parsed); setEditBuffer(raw); setError('');
      } catch (e: any) { setError(e.message); }
    })();
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

  if (error) return <div className="text-center py-20 text-red-400">{error}</div>;
  if (!cfg) return <div className="text-center py-20 text-gray-500">Loading...</div>;

  const sections: { id: Section; label: string }[] = [
    { id: 'pool', label: 'Pool' },
    { id: 'cleanup', label: 'Cleanup' },
    { id: 'health', label: 'Health' },
    { id: 'network', label: 'Network' },
    { id: 'artifacts', label: 'Artifacts' },
    { id: 'node', label: 'Node' },
    { id: 'api', label: 'API' },
    { id: 'raw', label: 'Raw YAML' },
  ];

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Settings</h1>
        {message && <span className="text-sm text-emerald-400">{message}</span>}
      </div>

      {/* Section tabs */}
      <div className="flex gap-1 border-b border-gray-800 pb-px overflow-x-auto">
        {sections.map(s => (
          <button key={s.id} onClick={() => { setActiveSection(s.id); if (s.id === 'raw') setEditBuffer(configYaml); }}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition whitespace-nowrap ${
              activeSection === s.id ? 'border-emerald-400 text-emerald-400' : 'border-transparent text-gray-500 hover:text-gray-300'
            }`}>
            {s.label}
          </button>
        ))}
      </div>

      {/* Pool */}
      {activeSection === 'pool' && (
        <FormSection title="Pool Configuration" description="Controls how many emulators can run and session behavior.">
          <Field label="Max Concurrent" value={cfg.Pool?.MaxConcurrent} type="number" hint="Maximum emulators running at once. Hardware-bound." />
          <Field label="Session Timeout" value={cfg.Pool?.SessionTimeoutMinutes} type="number" unit="minutes" hint="Default session duration before auto-release." />
          <Field label="Max Session Duration" value={cfg.Pool?.SessionMaxMinutes} type="number" unit="minutes" hint="Hard cap on session length." />
          <Field label="Queue Size" value={cfg.Pool?.QueueMaxSize} type="number" hint="Max pending session requests when pool is full." />
          <Field label="Queue Timeout" value={cfg.Pool?.QueueTimeoutSeconds} type="number" unit="seconds" hint="How long a request waits in queue." />
          <Field label="Port Range Min" value={cfg.Pool?.PortRangeMin} type="number" hint="Start of ADB port range (even numbers)." />
          <Field label="Port Range Max" value={cfg.Pool?.PortRangeMax} type="number" hint="End of ADB port range." />
        </FormSection>
      )}

      {/* Cleanup */}
      {activeSection === 'cleanup' && (
        <FormSection title="Cleanup & Idle" description="When and how devices are shut down.">
          <Field label="Idle Timeout" value={cfg.Cleanup?.IdleTimeoutMinutes || 5} type="number" unit="minutes" hint="Shut down warm emulators after this idle time." />
          <Field label="On Session End" value={cfg.Cleanup?.OnSessionEnd} type="string" hint="Reset strategy: snapshot_restore or app_uninstall." />
          <Field label="Orphan Check Interval" value={cfg.Cleanup?.OrphanCheckIntervalSecs} type="number" unit="seconds" hint="How often to scan for orphaned processes." />
          <Field label="Disk Cleanup Threshold" value={cfg.Cleanup?.DiskCleanupThresholdGB} type="number" unit="GB" hint="Clean old artifacts when free disk drops below this." />
        </FormSection>
      )}

      {/* Health */}
      {activeSection === 'health' && (
        <FormSection title="Health Checks" description="How devices are monitored for failures.">
          <Field label="Check Interval" value={cfg.HealthCheck?.IntervalSeconds} type="number" unit="seconds" hint="Time between health checks per device." />
          <Field label="Unhealthy Threshold" value={cfg.HealthCheck?.UnhealthyThreshold} type="number" hint="Consecutive failures before marking device as error." />
        </FormSection>
      )}

      {/* Network */}
      {activeSection === 'network' && (
        <FormSection title="Network" description="LAN access and service discovery.">
          <Field label="mDNS Enabled" value={cfg.Network?.MDNS?.Enabled ? 'true' : 'false'} type="boolean" hint="Announce farm on LAN via Bonjour." />
          <Field label="mDNS Service Type" value={cfg.Network?.MDNS?.ServiceType} type="string" hint="Bonjour service identifier." />
          <Field label="Allowed CIDRs" value={(cfg.Network?.AllowedCIDRs || []).join(', ')} type="string" hint="IP ranges allowed to connect. Comma separated." />
        </FormSection>
      )}

      {/* Artifacts */}
      {activeSection === 'artifacts' && (
        <FormSection title="Artifacts" description="What gets captured during sessions.">
          <Field label="Video Recording" value={cfg.Artifacts?.VideoRecording ? 'true' : 'false'} type="boolean" hint="Record screen during sessions." />
          <Field label="Screenshot on Failure" value={cfg.Artifacts?.ScreenshotOnFail ? 'true' : 'false'} type="boolean" hint="Auto-capture on test failure." />
          <Field label="Logcat Capture" value={cfg.Artifacts?.LogcatCapture ? 'true' : 'false'} type="boolean" hint="Capture Android logcat." />
          <Field label="Network HAR" value={cfg.Artifacts?.NetworkHARCapture ? 'true' : 'false'} type="boolean" hint="Capture network traffic as HAR." />
          <Field label="Retention" value={cfg.Artifacts?.RetentionDays} type="number" unit="days" hint="Delete artifacts older than this." />
        </FormSection>
      )}

      {/* Node */}
      {activeSection === 'node' && (
        <FormSection title="Node" description="This machine's identity and logging.">
          <Field label="Node Name" value={cfg.Node?.Name} type="string" hint="Empty = auto-detected from hostname." />
          <Field label="Log Level" value={cfg.Node?.LogLevel} type="string" hint="debug, info, warn, error." />
          <Field label="Data Directory" value={cfg.Node?.DataDir} type="string" hint="Where config, artifacts, and state are stored." />
          <Field label="Metrics Port" value={cfg.Node?.MetricsPort} type="number" hint="Prometheus metrics endpoint port." />
        </FormSection>
      )}

      {/* API */}
      {activeSection === 'api' && (
        <FormSection title="API Server" description="HTTP and gRPC endpoints.">
          <Field label="Host" value={cfg.API?.Host} type="string" hint="Bind address. 0.0.0.0 = all interfaces." />
          <Field label="REST Port" value={cfg.API?.Port} type="number" hint="HTTP API + Dashboard port." />
          <Field label="gRPC Port" value={cfg.API?.GRPCPort} type="number" hint="gRPC endpoint for SDK clients." />
        </FormSection>
      )}

      {/* Raw YAML */}
      {activeSection === 'raw' && (
        <div className="space-y-4">
          <div className="bg-gray-900 border border-gray-800 rounded-lg overflow-hidden">
            <div className="px-4 py-2 bg-gray-800/50 border-b border-gray-800 flex items-center justify-between">
              <span className="text-xs text-gray-400 font-mono">~/.drizz-farm/config.yaml</span>
              <span className="text-xs text-gray-600">{editBuffer.split('\n').length} lines</span>
            </div>
            <textarea value={editBuffer} onChange={e => setEditBuffer(e.target.value)}
              className="w-full h-[500px] bg-gray-950 p-4 text-sm font-mono text-gray-200 focus:outline-none resize-none leading-6"
              spellCheck={false} />
          </div>
          <div className="flex items-center justify-between">
            <p className="text-xs text-gray-500">Restart daemon after saving to apply changes.</p>
            <div className="flex gap-3">
              <button onClick={() => setEditBuffer(configYaml)} className="px-4 py-2 bg-gray-800 rounded text-sm hover:bg-gray-700 transition">Reset</button>
              <button onClick={handleSaveRaw} disabled={saving || editBuffer === configYaml}
                className="px-4 py-2 bg-emerald-500 text-white rounded text-sm font-medium hover:bg-emerald-400 transition disabled:opacity-50">
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
    <div className="bg-gray-900 border border-gray-800 rounded-lg">
      <div className="px-5 py-4 border-b border-gray-800">
        <h2 className="text-base font-semibold text-gray-200">{title}</h2>
        <p className="text-sm text-gray-500 mt-0.5">{description}</p>
      </div>
      <div className="divide-y divide-gray-800/50">
        {children}
      </div>
    </div>
  );
}

function Field({ label, value, type, unit, hint }: { label: string; value: any; type: string; unit?: string; hint?: string }) {
  const typeColors: Record<string, string> = {
    number: 'text-blue-400',
    string: 'text-amber-400',
    boolean: value === 'true' ? 'text-emerald-400' : 'text-red-400',
  };

  const typeBadge: Record<string, string> = {
    number: 'bg-blue-400/10 text-blue-400',
    string: 'bg-amber-400/10 text-amber-400',
    boolean: 'bg-purple-400/10 text-purple-400',
  };

  return (
    <div className="px-5 py-3 flex items-start gap-4">
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-gray-300">{label}</span>
          <span className={`text-[10px] px-1.5 py-0.5 rounded font-mono ${typeBadge[type] || typeBadge.string}`}>{type}</span>
        </div>
        {hint && <p className="text-xs text-gray-600 mt-0.5">{hint}</p>}
      </div>
      <div className="text-right flex-shrink-0">
        <span className={`text-sm font-mono ${typeColors[type] || 'text-gray-200'}`}>
          {String(value ?? 'not set')}
        </span>
        {unit && <span className="text-xs text-gray-600 ml-1">{unit}</span>}
      </div>
    </div>
  );
}
