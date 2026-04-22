import { useEffect, useState } from 'react';
import { api, SystemImage } from '../lib/api';
import { DeviceDef, enrichDevices, groupByCategory } from '../lib/devices';

type Step = 'device' | 'image' | 'configure' | 'creating' | 'done';

export function CreateWizard({ isModal, onClose }: { isModal?: boolean; onClose?: () => void } = {}) {
  const [step, setStep] = useState<Step>('device');
  const [images, setImages] = useState<SystemImage[]>([]);
  const [deviceDefs, setDeviceDefs] = useState<DeviceDef[]>([]);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [availableImages, setAvailableImages] = useState<SystemImage[]>([]);
  const [selectedDevice, setSelectedDevice] = useState<DeviceDef | null>(null);
  const [selectedImage, setSelectedImage] = useState<SystemImage | null>(null);
  const [count, setCount] = useState(3);
  const [profileName, setProfileName] = useState('');
  const [activeCategory, setActiveCategory] = useState('phone');
  const [installing, setInstalling] = useState<string | null>(null);
  const [createResult, setCreateResult] = useState<{ created: number; errors: string[] } | null>(null);

  useEffect(() => { loadData(); }, []);

  const loadData = async () => {
    try {
      const [i, ai, d] = await Promise.all([api.systemImages(), api.availableImages(), api.devices()]);
      setImages(i.images || []); setAvailableImages(ai.images || []);
      setDeviceDefs(enrichDevices(d.devices || []));
      setError('');
    } catch (e: any) { setError(e.message); }
    setLoading(false);
  };

  const handleInstall = async (path: string) => {
    setInstalling(path);
    try { await api.installImage(path); await loadData(); } catch (e: any) { alert(e.message); }
    setInstalling(null);
  };

  const handleCreate = async () => {
    if (!selectedImage || !selectedDevice) return;
    setStep('creating');
    try {
      const result = await api.createAVDs({ profile_name: profileName, device: selectedDevice.id, system_image: selectedImage.path, count });
      setCreateResult(result); setStep('done');
    } catch (e: any) { setError(e.message); setStep('configure'); }
  };

  if (loading) return <div className="flex items-center justify-center py-20"><div className="w-5 h-5 border-2 border-muted border-t-primary rounded-full animate-spin" /><span className="ml-3 text-muted-foreground text-sm">Loading SDK info...</span></div>;
  if (error && !['creating', 'done'].includes(step)) return <div className="text-center py-20"><div className="text-destructive text-base mb-2">Error</div><div className="text-muted-foreground text-sm">{error}</div></div>;

  const steps = ['device', 'image', 'configure', 'done'];
  const stepLabels = ['Device', 'System Image', 'Configure', 'Done'];
  const currentIdx = steps.indexOf(step === 'creating' ? 'done' : step);
  const groups = groupByCategory(deviceDefs);
  const installedPaths = new Set(images.map(i => i.path));
  const apiLevel = (name: string) => { const m = name.match(/android-(\d+)/); return m ? parseInt(m[1]) : 0; };

  return (
    <div className={isModal ? "" : "max-w-4xl mx-auto animate-fade-in"}>
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className={`${isModal ? 'text-lg' : 'text-xl'} font-bold text-foreground`}>Virtual Device Configuration</h1>
          <p className="text-muted-foreground text-sm mt-1">Select hardware, system image, and configure your emulators.</p>
        </div>
        {isModal && onClose && (
          <button onClick={onClose} className="text-muted-foreground hover:text-foreground transition text-xl px-2">×</button>
        )}
      </div>

      {/* Step indicator */}
      <div className="flex items-center gap-2 mb-8">
        {steps.map((s, i) => (
          <div key={s} className="flex items-center gap-2 flex-1">
            <div className={`w-7 h-7 rounded-full flex items-center justify-center text-xs font-bold border-2 transition
              ${currentIdx > i ? 'bg-primary border-primary text-primary-foreground' :
                currentIdx === i ? 'border-primary text-primary' :
                'border-border text-muted-foreground'}`}>
              {currentIdx > i ? '✓' : i + 1}
            </div>
            <span className={`text-sm ${currentIdx >= i ? 'text-foreground' : 'text-muted-foreground'}`}>{stepLabels[i]}</span>
            {i < steps.length - 1 && <div className={`flex-1 h-px ${currentIdx > i ? 'bg-primary' : 'bg-border'}`} />}
          </div>
        ))}
      </div>

      {/* Step 1: Device */}
      {step === 'device' && (
        <div>
          <div className="flex gap-1 mb-4 overflow-x-auto pb-2">
            {groups.map(g => (
              <button key={g.category} onClick={() => setActiveCategory(g.category)}
                className={`px-3 py-1.5 rounded-md text-sm whitespace-nowrap transition ${
                  activeCategory === g.category ? 'surface-3 text-foreground' : 'text-muted-foreground hover:text-foreground'
                }`}>
                {g.category.charAt(0).toUpperCase() + g.category.slice(1)} ({g.devices.length})
              </button>
            ))}
          </div>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            {groups.filter(g => g.category === activeCategory).flatMap(g => g.devices).map(dev => (
              <button key={dev.id} onClick={() => { setSelectedDevice(dev); setStep('image'); }}
                className={`text-left p-4 rounded-xl border transition-all duration-150 hover:border-primary/50 ${
                  selectedDevice?.id === dev.id ? 'border-primary surface-2' : 'border-border surface-1'
                }`}>
                <div className="flex items-start justify-between">
                  <div>
                    <div className="font-medium text-foreground text-sm">{dev.name}</div>
                    {dev.screen && <div className="text-xs text-muted-foreground mt-1">{dev.screen}</div>}
                    {dev.density && <div className="text-[11px] text-muted-foreground/60">{dev.density}</div>}
                  </div>
                  {dev.popular && <span className="badge badge-warm">Popular</span>}
                </div>
                {dev.year && <div className="text-[11px] text-muted-foreground/50 mt-2">{dev.year}</div>}
              </button>
            ))}
          </div>
        </div>
      )}

      {/* Step 2: System Image */}
      {step === 'image' && (
        <div className="space-y-4">
          <div className="flex items-center gap-3">
            <button onClick={() => setStep('device')} className="text-sm text-muted-foreground hover:text-foreground">← Back</button>
            <span className="text-sm text-muted-foreground">Device: <span className="text-primary">{selectedDevice?.name}</span></span>
          </div>
          <h2 className="text-base font-semibold text-foreground">System Image</h2>
          <div className="section-card overflow-hidden">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-muted-foreground text-[10px] uppercase tracking-wider">
                  <th className="text-left px-4 py-2.5 font-medium">API Level</th>
                  <th className="text-left px-4 py-2.5 font-medium">Target</th>
                  <th className="text-left px-4 py-2.5 font-medium">Arch</th>
                  <th className="text-right px-4 py-2.5 font-medium">Status</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border/50">
                {images.map(img => (
                  <tr key={img.path} onClick={() => {
                    setSelectedImage(img);
                    let name = img.api_name.replace('android-', 'api').replace(/-/g, '_');
                    if (img.variant.includes('playstore')) name += '_play';
                    setProfileName(name);
                    setStep('configure');
                  }} className="cursor-pointer card-hover">
                    <td className="px-4 py-3 font-medium text-foreground">API {img.api_name.replace('android-', '')}</td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {img.variant.replace(/_/g, ' ')}
                      {img.variant.includes('playstore') && <span className="ml-2 badge badge-allocated">Play Store</span>}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground font-mono text-xs">{img.arch}</td>
                    <td className="px-4 py-3 text-right"><span className="badge badge-warm">Installed</span></td>
                  </tr>
                ))}
                {availableImages.filter(i => !installedPaths.has(i.path) && apiLevel(i.api_name) >= 28)
                  .sort((a, b) => apiLevel(b.api_name) - apiLevel(a.api_name))
                  .map(img => (
                  <tr key={img.path} className="opacity-60">
                    <td className="px-4 py-3 text-muted-foreground">API {img.api_name.replace('android-', '')}</td>
                    <td className="px-4 py-3 text-muted-foreground">{img.variant.replace(/_/g, ' ')}</td>
                    <td className="px-4 py-3 text-muted-foreground font-mono text-xs">{img.arch}</td>
                    <td className="px-4 py-3 text-right">
                      <button onClick={(e) => { e.stopPropagation(); handleInstall(img.path); }} disabled={installing === img.path}
                        className="action-btn action-btn-accent text-[10px] disabled:opacity-40">
                        {installing === img.path ? 'Installing...' : 'Install'}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Step 3: Configure */}
      {step === 'configure' && (
        <div className="space-y-4">
          <div className="flex items-center gap-3">
            <button onClick={() => setStep('image')} className="text-sm text-muted-foreground hover:text-foreground">← Back</button>
            <span className="text-sm text-muted-foreground">
              {selectedDevice?.name} · API {selectedImage?.api_name.replace('android-', '')}
            </span>
          </div>
          <div className="section-card divide-y divide-border/50">
            <div className="px-5 py-4">
              <label className="text-sm font-medium text-foreground">Profile Name</label>
              <input value={profileName} onChange={e => setProfileName(e.target.value)}
                className="w-full mt-2 px-3 py-2 rounded-lg surface-2 border border-border text-sm font-mono text-foreground focus:outline-none focus:ring-1 focus:ring-primary" />
              <p className="text-[11px] text-muted-foreground mt-1">Used as AVD name prefix</p>
            </div>
            <div className="px-5 py-4">
              <label className="text-sm font-medium text-foreground">Count</label>
              <div className="flex items-center gap-3 mt-2">
                {[1, 2, 3, 5, 10].map(n => (
                  <button key={n} onClick={() => setCount(n)}
                    className={`w-10 h-10 rounded-lg text-sm font-mono transition ${
                      count === n ? 'surface-3 text-primary border border-primary/30' : 'surface-2 text-muted-foreground border border-border hover:text-foreground'
                    }`}>{n}</button>
                ))}
              </div>
            </div>
          </div>
          <button onClick={handleCreate} className="action-btn action-btn-primary text-sm px-6 py-2.5">
            Create {count} Emulator{count > 1 ? 's' : ''}
          </button>
        </div>
      )}

      {/* Creating */}
      {step === 'creating' && (
        <div className="text-center py-16">
          <div className="w-8 h-8 border-2 border-muted border-t-primary rounded-full animate-spin mx-auto mb-4" />
          <div className="text-foreground">Creating emulators...</div>
        </div>
      )}

      {/* Done */}
      {step === 'done' && createResult && (
        <div className="text-center py-16">
          <div className="text-4xl mb-4">✓</div>
          <div className="text-foreground text-lg font-semibold">{createResult.created} emulator{createResult.created > 1 ? 's' : ''} created</div>
          {createResult.errors.length > 0 && (
            <div className="mt-3 text-sm text-destructive">{createResult.errors.join(', ')}</div>
          )}
          <button onClick={() => { if (onClose) onClose(); else setStep('device'); }}
            className="action-btn action-btn-primary mt-6 px-6 py-2.5">
            {isModal ? 'Close' : 'Create More'}
          </button>
        </div>
      )}
    </div>
  );
}
