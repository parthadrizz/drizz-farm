import { useEffect, useState } from 'react';
import { api } from '../lib/api';

export function Settings() {
  const [configYaml, setConfigYaml] = useState('');
  const [configParsed, setConfigParsed] = useState<any>(null);
  const [editing, setEditing] = useState(false);
  const [editBuffer, setEditBuffer] = useState('');
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');

  useEffect(() => {
    loadConfig();
  }, []);

  const loadConfig = async () => {
    try {
      const [raw, parsed] = await Promise.all([api.getConfigRaw(), api.getConfig()]);
      setConfigYaml(raw);
      setConfigParsed(parsed);
      setEditBuffer(raw);
      setError('');
    } catch (e: any) {
      setError(e.message);
    }
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      const result = await api.saveConfigRaw(editBuffer);
      setMessage(result.message || 'Saved');
      setConfigYaml(editBuffer);
      setEditing(false);
      setTimeout(() => setMessage(''), 5000);
    } catch (e: any) {
      setError(e.message);
    }
    setSaving(false);
  };

  if (error) return (
    <div className="text-center py-20">
      <div className="text-red-400 mb-2">{error}</div>
      <button onClick={loadConfig} className="px-4 py-2 bg-gray-800 rounded text-sm">Retry</button>
    </div>
  );

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Settings</h1>
        {message && <span className="text-sm text-emerald-400">{message}</span>}
      </div>

      {/* Quick view */}
      {configParsed && !editing && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <ConfigCard title="Pool" items={[
            { label: 'Max Concurrent', value: configParsed.pool?.max_concurrent },
            { label: 'Session Timeout', value: `${configParsed.pool?.session_timeout_minutes}m` },
            { label: 'Max Duration', value: `${configParsed.pool?.session_max_duration_minutes}m` },
            { label: 'Queue Size', value: configParsed.pool?.queue_max_size },
            { label: 'Port Range', value: `${configParsed.pool?.port_range_min}-${configParsed.pool?.port_range_max}` },
          ]} />

          <ConfigCard title="Cleanup" items={[
            { label: 'Idle Timeout', value: `${configParsed.cleanup?.idle_timeout_minutes || 5}m` },
            { label: 'On Session End', value: configParsed.cleanup?.on_session_end },
            { label: 'Disk Cleanup', value: `${configParsed.cleanup?.disk_cleanup_threshold_gb}GB` },
          ]} />

          <ConfigCard title="Health Check" items={[
            { label: 'Interval', value: `${configParsed.health_check?.interval_seconds}s` },
            { label: 'Unhealthy Threshold', value: configParsed.health_check?.unhealthy_threshold },
          ]} />

          <ConfigCard title="Network" items={[
            { label: 'mDNS', value: configParsed.network?.mdns?.enabled ? 'Enabled' : 'Disabled' },
            { label: 'API Port', value: configParsed.api?.port },
            { label: 'Allowed CIDRs', value: (configParsed.network?.allowed_cidrs || []).length + ' ranges' },
          ]} />

          <ConfigCard title="Artifacts" items={[
            { label: 'Video Recording', value: configParsed.artifacts?.video_recording ? 'Yes' : 'No' },
            { label: 'Screenshot on Fail', value: configParsed.artifacts?.screenshot_on_failure ? 'Yes' : 'No' },
            { label: 'Retention', value: `${configParsed.artifacts?.retention_days}d` },
          ]} />

          <ConfigCard title="License" items={[
            { label: 'Key', value: configParsed.license?.key || 'Not set' },
            { label: 'Endpoint', value: configParsed.license?.validation_endpoint },
          ]} />
        </div>
      )}

      {/* Edit button */}
      {!editing && (
        <button onClick={() => { setEditing(true); setEditBuffer(configYaml); }}
          className="px-4 py-2 bg-gray-800 border border-gray-700 rounded text-sm hover:bg-gray-700 transition">
          Edit Raw Config
        </button>
      )}

      {/* Editor */}
      {editing && (
        <div className="space-y-3">
          <textarea
            value={editBuffer}
            onChange={e => setEditBuffer(e.target.value)}
            className="w-full h-96 bg-gray-900 border border-gray-700 rounded-lg p-4 text-sm font-mono text-gray-200 focus:outline-none focus:border-emerald-400 resize-y"
            spellCheck={false}
          />
          <div className="flex gap-3">
            <button onClick={() => setEditing(false)}
              className="px-4 py-2 bg-gray-800 rounded text-sm hover:bg-gray-700 transition">Cancel</button>
            <button onClick={handleSave} disabled={saving}
              className="px-4 py-2 bg-emerald-500 text-white rounded text-sm font-medium hover:bg-emerald-400 transition disabled:opacity-50">
              {saving ? 'Saving...' : 'Save Config'}
            </button>
          </div>
          <p className="text-xs text-gray-500">Changes take effect after daemon restart.</p>
        </div>
      )}
    </div>
  );
}

function ConfigCard({ title, items }: { title: string; items: { label: string; value: any }[] }) {
  return (
    <div className="bg-gray-900 rounded-lg border border-gray-800 p-4">
      <h3 className="text-sm font-semibold text-gray-400 uppercase tracking-wider mb-3">{title}</h3>
      <div className="space-y-2">
        {items.map(item => (
          <div key={item.label} className="flex justify-between text-sm">
            <span className="text-gray-500">{item.label}</span>
            <span className="text-gray-200">{String(item.value ?? '-')}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
