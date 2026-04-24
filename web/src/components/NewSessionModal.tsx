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

// A selectable row in the device dropdown. Either a live tracked
// instance (warm / allocated / etc.) or a cold AVD we know about from
// avdmanager but which isn't booted yet. Picking a cold one boots it
// on demand (pool.AllocateWith now handles that server-side).
interface DeviceRow {
  key: string;            // unique react key + select value
  label: string;          // human label shown in the option
  instanceId?: string;    // set when this is a live tracked instance
  avdName?: string;       // set when the row is an AVD known only by name
  available: boolean;     // false when warm-but-allocated / busy
  profile?: string;
}

export function NewSessionModal({ open, onClose, onCreated }: Props) {
  const [rows, setRows] = useState<DeviceRow[]>([]);
  const [selectedKey, setSelectedKey] = useState<string>('');
  const [profile, setProfile] = useState<string>('');
  const [timeoutMin, setTimeoutMin] = useState(60);
  const [recordVideo, setRecordVideo] = useState(true);
  const [captureLogcat, setCaptureLogcat] = useState(true);
  const [captureScreenshots, setCaptureScreenshots] = useState(true);
  const [captureNetwork, setCaptureNetwork] = useState(false);
  const [retentionHours, setRetentionHours] = useState(0);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!open) return;
    setLoading(true);
    setError('');
    // Fetch live instances + all known AVDs and merge. Without the
    // discovery call, the dropdown was empty until you'd already
    // booted an emulator somewhere else first — chicken-and-egg
    // nonsense for first-time users.
    Promise.all([
      api.poolDevices().catch(() => ({ devices: [] as DeviceInstance[] })),
      api.avds().catch(() => ({ avds: [] as { name: string; display_name?: string }[] })),
    ]).then(([poolResp, avdsResp]) => {
      const instances = poolResp.devices || [];
      const avds = avdsResp.avds || [];
      const merged: DeviceRow[] = [];
      const seenAVDs = new Set<string>();

      // Live instances first (they're cheaper — already booted).
      for (const inst of instances) {
        const avdName = inst.device_name;
        if (avdName) seenAVDs.add(avdName);
        const busy = inst.state !== 'warm';
        merged.push({
          key: `inst:${inst.id}`,
          label: `${avdName || inst.id} · ${inst.state}${inst.reserved ? ' · reserved' : ''}`,
          instanceId: inst.id,
          avdName,
          available: inst.state === 'warm' && (!inst.reserved || true),
          profile: inst.profile,
        });
        void busy;
      }
      // Then cold AVDs that aren't already tracked.
      for (const avd of avds) {
        if (seenAVDs.has(avd.name)) continue;
        merged.push({
          key: `avd:${avd.name}`,
          label: `${avd.display_name || avd.name} · cold (will boot)`,
          avdName: avd.name,
          available: true,
        });
      }
      setRows(merged);
      // Pre-select the first available device so Create works on first click.
      const firstAvailable = merged.find(r => r.available);
      if (firstAvailable) setSelectedKey(firstAvailable.key);
      setLoading(false);
    });
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
    const row = rows.find(r => r.key === selectedKey);
    if (row?.instanceId) {
      body.device_id = row.instanceId;
    } else if (row?.avdName) {
      body.avd_name = row.avdName;
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

  const profiles = Array.from(new Set(rows.map(r => r.profile).filter(Boolean))) as string[];

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
          {/* Device picker — shows every known AVD, warm or cold. */}
          <div>
            <label className="text-xs text-muted-foreground block mb-1.5">Device</label>
            {loading ? (
              <div className="text-xs text-muted-foreground">Loading devices…</div>
            ) : rows.length === 0 ? (
              <div className="text-xs text-muted-foreground">
                No AVDs found on this machine. Create one from the Dashboard → Add button first.
              </div>
            ) : (
              <select
                value={selectedKey}
                onChange={e => { setSelectedKey(e.target.value); if (e.target.value) setProfile(''); }}
                className="w-full surface-2 border border-border rounded-lg px-3 py-2 text-sm text-foreground"
              >
                <option value="">— match by profile below —</option>
                {rows.map(r => (
                  <option key={r.key} value={r.key} disabled={!r.available}>
                    {r.label}
                  </option>
                ))}
              </select>
            )}
            <p className="text-[11px] text-muted-foreground mt-1">
              Cold AVDs will be booted automatically. Picking a warm one is instant.
            </p>
          </div>

          {!selectedKey && profiles.length > 0 && (
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
