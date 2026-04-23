/**
 * NewSessionModal — opens a session against a chosen device with
 * declarative capture capabilities. Picks:
 *   - profile / specific device
 *   - record_video / capture_logcat / capture_screenshots / capture_network
 *   - timeout minutes
 *
 * On submit, POSTs /api/v1/sessions with the capabilities block. The
 * session shows up immediately in the Sessions list via the parent's
 * refresh callback, and the <ArtifactsPanel> on that row surfaces
 * anything captured once the session releases.
 */
import { useEffect, useState } from 'react';
import { Video, FileText, Camera, Network, X } from 'lucide-react';
import { api, DeviceInstance } from '../lib/api';

interface Props {
  open: boolean;
  onClose: () => void;
  onCreated: () => void;
}

export function NewSessionModal({ open, onClose, onCreated }: Props) {
  const [devices, setDevices] = useState<DeviceInstance[]>([]);
  const [selectedDevice, setSelectedDevice] = useState<string>(''); // '' = any (by profile)
  const [profile, setProfile] = useState<string>('');
  const [timeoutMin, setTimeoutMin] = useState(60);
  const [recordVideo, setRecordVideo] = useState(true);
  const [captureLogcat, setCaptureLogcat] = useState(true);
  const [captureScreenshots, setCaptureScreenshots] = useState(true);
  const [captureNetwork, setCaptureNetwork] = useState(false);
  const [retentionHours, setRetentionHours] = useState(0); // 0 = daemon default
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!open) return;
    api.poolDevices({ free: true })
      .then(d => setDevices(d.devices || []))
      .catch(() => setDevices([]));
  }, [open]);

  if (!open) return null;

  const handleCreate = async () => {
    setSubmitting(true);
    setError('');
    const body: any = {
      source: 'dashboard',
      timeout_minutes: timeoutMin,
      capabilities: {
        record_video: recordVideo,
        capture_logcat: captureLogcat,
        capture_screenshots: captureScreenshots,
        capture_network: captureNetwork,
        ...(retentionHours > 0 ? { retention_hours: retentionHours } : {}),
      },
    };
    if (selectedDevice) {
      body.device_id = selectedDevice;
    } else if (profile) {
      body.profile = profile;
    } else {
      setError('Pick a device or a profile');
      setSubmitting(false);
      return;
    }
    try {
      const res = await fetch('/api/v1/sessions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      if (!res.ok) {
        const err = await res.json().catch(() => ({ message: 'request failed' }));
        throw new Error(err.message || `HTTP ${res.status}`);
      }
      onCreated();
    } catch (e: any) {
      setError(e?.message || 'create failed');
    } finally {
      setSubmitting(false);
    }
  };

  const profiles = Array.from(new Set(devices.map(d => d.profile).filter(Boolean)));

  return (
    <div
      onClick={onClose}
      className="fixed inset-0 z-50 flex items-center justify-center p-4"
      style={{ background: 'hsl(var(--surface-0) / 0.7)' }}
    >
      <div
        onClick={e => e.stopPropagation()}
        className="surface-1 border border-border rounded-xl shadow-2xl max-w-xl w-full p-6 animate-fade-in"
      >
        <div className="flex items-center justify-between mb-5">
          <h2 className="text-base font-semibold text-foreground">New session</h2>
          <button onClick={onClose} className="p-1 rounded hover:bg-surface-2 text-muted-foreground hover:text-foreground">
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="space-y-4">
          {/* Device picker */}
          <div>
            <label className="text-xs text-muted-foreground block mb-1.5">Device</label>
            <select
              value={selectedDevice}
              onChange={e => { setSelectedDevice(e.target.value); if (e.target.value) setProfile(''); }}
              className="w-full surface-2 border border-border rounded-lg px-3 py-2 text-sm text-foreground"
            >
              <option value="">— any matching profile —</option>
              {devices.map(d => (
                <option key={d.id} value={d.id}>
                  {d.device_name} ({d.profile}) · {d.state}{d.reserved ? ' · reserved' : ''}
                </option>
              ))}
            </select>
            <p className="text-[11px] text-muted-foreground mt-1">
              Pick a specific one, or leave blank to match by profile below.
            </p>
          </div>

          {!selectedDevice && (
            <div>
              <label className="text-xs text-muted-foreground block mb-1.5">Profile</label>
              <select
                value={profile}
                onChange={e => setProfile(e.target.value)}
                className="w-full surface-2 border border-border rounded-lg px-3 py-2 text-sm text-foreground"
              >
                <option value="">— first available profile —</option>
                {profiles.map(p => <option key={p} value={p}>{p}</option>)}
              </select>
            </div>
          )}

          {/* Capabilities */}
          <div>
            <label className="text-xs text-muted-foreground block mb-2">Capture</label>
            <div className="grid grid-cols-2 gap-2">
              <Capability icon={Video} label="Record video" checked={recordVideo} onChange={setRecordVideo}
                hint="Screenrecord, auto-stitched into video.mp4" />
              <Capability icon={FileText} label="Capture logcat" checked={captureLogcat} onChange={setCaptureLogcat}
                hint="All buffers → logcat.txt" />
              <Capability icon={Camera} label="Screenshots" checked={captureScreenshots} onChange={setCaptureScreenshots}
                hint="Gate for on-demand screenshot API" />
              <Capability icon={Network} label="Network (HAR)" checked={captureNetwork} onChange={setCaptureNetwork}
                hint="mitmproxy required — brew install mitmproxy" />
            </div>
          </div>

          {/* Timeout + retention */}
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="text-xs text-muted-foreground block mb-1.5">Timeout (min)</label>
              <input
                type="number" min={1} max={480} value={timeoutMin}
                onChange={e => setTimeoutMin(parseInt(e.target.value) || 60)}
                className="w-full surface-2 border border-border rounded-lg px-3 py-2 text-sm text-foreground"
              />
            </div>
            <div>
              <label className="text-xs text-muted-foreground block mb-1.5">Retention (hours, 0 = default)</label>
              <input
                type="number" min={0} max={8760} value={retentionHours}
                onChange={e => setRetentionHours(parseInt(e.target.value) || 0)}
                className="w-full surface-2 border border-border rounded-lg px-3 py-2 text-sm text-foreground"
              />
            </div>
          </div>

          {error && <div className="text-xs text-destructive">{error}</div>}

          <div className="flex items-center justify-end gap-3 pt-2">
            <button onClick={onClose} className="action-btn surface-3 text-foreground">Cancel</button>
            <button
              onClick={handleCreate}
              disabled={submitting}
              className="action-btn action-btn-primary disabled:opacity-40"
            >
              {submitting ? 'Creating…' : 'Create session'}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

function Capability({
  icon: Icon,
  label,
  checked,
  onChange,
  hint,
}: {
  icon: any;
  label: string;
  checked: boolean;
  onChange: (v: boolean) => void;
  hint: string;
}) {
  return (
    <button
      type="button"
      onClick={() => onChange(!checked)}
      title={hint}
      className={`text-left p-3 rounded-lg border transition-all flex items-start gap-2 ${
        checked ? 'border-primary surface-2' : 'border-border surface-1 hover:border-primary/40'
      }`}
    >
      <Icon className={`w-4 h-4 mt-0.5 flex-shrink-0 ${checked ? 'text-primary' : 'text-muted-foreground'}`} />
      <div className="flex-1 min-w-0">
        <div className={`text-xs font-medium ${checked ? 'text-foreground' : 'text-muted-foreground'}`}>{label}</div>
        <div className="text-[10px] text-muted-foreground mt-0.5 leading-relaxed">{hint}</div>
      </div>
    </button>
  );
}
