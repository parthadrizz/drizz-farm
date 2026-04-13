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
    { id: 'api', label: 'API' }, { id: 'raw', label: 'Raw YAML' },
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
        <FormSection title="Network" description="LAN access and service discovery.">
          <Field label="mDNS Enabled" value={cfg.Network?.MDNS?.Enabled ? 'true' : 'false'} />
          <Field label="mDNS Service Type" value={cfg.Network?.MDNS?.ServiceType} />
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
