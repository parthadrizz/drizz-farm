import { useEffect, useState } from 'react';
import { api } from '../lib/api';

export function Settings() {
  const [configYaml, setConfigYaml] = useState('');
  const [cfg, setCfg] = useState<any>(null);
  const [editing, setEditing] = useState(false);
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

  const handleSave = async () => {
    setSaving(true);
    try {
      const result = await api.saveConfigRaw(editBuffer);
      setMessage(result.message || 'Saved');
      setConfigYaml(editBuffer);
      setEditing(false);
      setTimeout(() => setMessage(''), 5000);
    } catch (e: any) { setError(e.message); }
    setSaving(false);
  };

  if (error) return (
    <div className="text-center py-20">
      <div className="text-red-400 mb-2">{error}</div>
    </div>
  );
  if (!cfg) return <div className="text-center py-20 text-gray-500">Loading...</div>;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Settings</h1>
        {message && <span className="text-sm text-emerald-400">{message}</span>}
      </div>

      {!editing && (
        <>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <Card title="Pool" items={[
              ['Max Concurrent', cfg.Pool?.MaxConcurrent],
              ['Session Timeout', `${cfg.Pool?.SessionTimeoutMinutes}m`],
              ['Max Duration', `${cfg.Pool?.SessionMaxMinutes}m`],
              ['Queue Size', cfg.Pool?.QueueMaxSize],
              ['Port Range', `${cfg.Pool?.PortRangeMin}-${cfg.Pool?.PortRangeMax}`],
            ]} />
            <Card title="Cleanup" items={[
              ['Idle Timeout', `${cfg.Cleanup?.IdleTimeoutMinutes || 5}m`],
              ['On Session End', cfg.Cleanup?.OnSessionEnd],
              ['Disk Cleanup', `${cfg.Cleanup?.DiskCleanupThresholdGB}GB`],
            ]} />
            <Card title="Health Check" items={[
              ['Interval', `${cfg.HealthCheck?.IntervalSeconds}s`],
              ['Unhealthy Threshold', cfg.HealthCheck?.UnhealthyThreshold],
            ]} />
            <Card title="Network" items={[
              ['mDNS', cfg.Network?.MDNS?.Enabled ? 'Enabled' : 'Disabled'],
              ['API Port', cfg.API?.Port],
              ['Allowed CIDRs', `${(cfg.Network?.AllowedCIDRs || []).length} ranges`],
            ]} />
            <Card title="Artifacts" items={[
              ['Video Recording', cfg.Artifacts?.VideoRecording ? 'Yes' : 'No'],
              ['Screenshot on Fail', cfg.Artifacts?.ScreenshotOnFail ? 'Yes' : 'No'],
              ['Retention', `${cfg.Artifacts?.RetentionDays}d`],
            ]} />
            <Card title="Node" items={[
              ['Name', cfg.Node?.Name || 'auto'],
              ['Log Level', cfg.Node?.LogLevel],
              ['Data Dir', cfg.Node?.DataDir],
            ]} />
          </div>

          <button onClick={() => { setEditing(true); setEditBuffer(configYaml); }}
            className="px-4 py-2 bg-gray-800 border border-gray-700 rounded text-sm hover:bg-gray-700 transition">
            Edit Raw Config
          </button>
        </>
      )}

      {editing && (
        <div className="space-y-3">
          <textarea value={editBuffer} onChange={e => setEditBuffer(e.target.value)}
            className="w-full h-96 bg-gray-900 border border-gray-700 rounded-lg p-4 text-sm font-mono text-gray-200 focus:outline-none focus:border-emerald-400 resize-y"
            spellCheck={false} />
          <div className="flex gap-3">
            <button onClick={() => setEditing(false)} className="px-4 py-2 bg-gray-800 rounded text-sm hover:bg-gray-700">Cancel</button>
            <button onClick={handleSave} disabled={saving}
              className="px-4 py-2 bg-emerald-500 text-white rounded text-sm font-medium hover:bg-emerald-400 disabled:opacity-50">
              {saving ? 'Saving...' : 'Save Config'}
            </button>
          </div>
          <p className="text-xs text-gray-500">Restart daemon to apply changes.</p>
        </div>
      )}
    </div>
  );
}

function Card({ title, items }: { title: string; items: [string, any][] }) {
  return (
    <div className="bg-gray-900 rounded-lg border border-gray-800 p-4">
      <h3 className="text-sm font-semibold text-gray-400 uppercase tracking-wider mb-3">{title}</h3>
      <div className="space-y-2">
        {items.map(([label, value]) => (
          <div key={label} className="flex justify-between text-sm">
            <span className="text-gray-500">{label}</span>
            <span className="text-gray-200">{String(value ?? '-')}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
