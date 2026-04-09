import { useEffect, useState } from 'react';
import { api, SystemImage } from '../lib/api';

type Step = 'image' | 'device' | 'count' | 'creating' | 'done';

export function CreateWizard() {
  const [step, setStep] = useState<Step>('image');
  const [images, setImages] = useState<SystemImage[]>([]);
  const [devices, setDevices] = useState<string[]>([]);
  const [avds, setAvds] = useState<string[]>([]);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [selectedImage, setSelectedImage] = useState<SystemImage | null>(null);
  const [selectedDevice, setSelectedDevice] = useState('');
  const [count, setCount] = useState(3);
  const [profileName, setProfileName] = useState('');
  const [createResult, setCreateResult] = useState<{ created: number; errors: string[] } | null>(null);

  useEffect(() => {
    (async () => {
      try {
        const [i, d, a] = await Promise.all([api.systemImages(), api.devices(), api.avds()]);
        setImages(i.images || []); setDevices(d.devices || []); setAvds((a.avds || []).map(x => x.name)); setError('');
      } catch (e: any) { setError(e.message); }
      setLoading(false);
    })();
  }, []);

  const handleCreate = async () => {
    if (!selectedImage || !selectedDevice) return;
    setStep('creating');
    try {
      const result = await api.createAVDs({ profile_name: profileName, device: selectedDevice, system_image: selectedImage.path, count });
      setCreateResult(result); setStep('done');
    } catch (e: any) { setError(e.message); setStep('count'); }
  };

  if (loading) return <div className="text-center py-20 text-gray-500">Loading SDK info...</div>;
  if (error && !['creating', 'done'].includes(step)) return (
    <div className="text-center py-20">
      <div className="text-red-400 text-lg mb-2">Error</div>
      <div className="text-gray-500 text-sm">{error}</div>
    </div>
  );

  const progress = ['image', 'device', 'count', 'done'];
  const stepIdx = progress.indexOf(step === 'creating' ? 'done' : step);

  return (
    <div className="max-w-2xl mx-auto">
      <h1 className="text-2xl font-bold mb-2">Create Emulator Farm</h1>
      <p className="text-gray-500 mb-8">Pick a system image, device, and how many to create.</p>
      <div className="flex gap-2 mb-8">{progress.map((_, i) => <div key={i} className={`h-1 flex-1 rounded-full ${stepIdx >= i ? 'bg-emerald-400' : 'bg-gray-800'}`} />)}</div>

      {step === 'image' && (
        <div className="space-y-3">
          <h2 className="text-lg font-semibold">Select System Image</h2>
          {images.length === 0 ? <div className="bg-gray-900 border border-gray-800 rounded-lg p-6 text-center text-gray-400">No system images found</div> :
            images.map(img => (
              <button key={img.path} onClick={() => { setSelectedImage(img); setProfileName(img.api_name.replace('android-','api').replace(/-/g,'_') + (img.variant.includes('playstore') ? '_play' : '')); setStep('device'); }}
                className="w-full text-left p-4 rounded-lg border border-gray-800 bg-gray-900 hover:border-emerald-400/50 transition">
                <div className="font-medium text-gray-200">{img.api_name}</div>
                <div className="text-sm text-gray-500 mt-1">{img.variant} · {img.arch}</div>
                <div className="text-xs text-gray-600 mt-1 font-mono">{img.path}</div>
              </button>
            ))}
        </div>
      )}

      {step === 'device' && (
        <div className="space-y-3">
          <h2 className="text-lg font-semibold">Select Device</h2>
          <div className="grid grid-cols-2 gap-2 max-h-96 overflow-y-auto">
            {devices.map(dev => (
              <button key={dev} onClick={() => { setSelectedDevice(dev); setStep('count'); }}
                className="text-left p-3 rounded-lg border border-gray-800 bg-gray-900 text-sm hover:border-emerald-400/50 transition">{dev}</button>
            ))}
          </div>
          <button onClick={() => setStep('image')} className="text-sm text-gray-500 hover:text-gray-300">← Back</button>
        </div>
      )}

      {step === 'count' && (
        <div className="space-y-6">
          <h2 className="text-lg font-semibold">Configure</h2>
          <div className="bg-gray-900 border border-gray-800 rounded-lg p-4 space-y-4">
            <div>
              <label className="block text-sm text-gray-400 mb-1">Profile Name</label>
              <input type="text" value={profileName} onChange={e => setProfileName(e.target.value)}
                className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm focus:outline-none focus:border-emerald-400" />
            </div>
            <div>
              <label className="block text-sm text-gray-400 mb-1">Number of Emulators</label>
              <div className="flex items-center gap-4">
                <input type="range" min={1} max={10} value={count} onChange={e => setCount(+e.target.value)} className="flex-1 accent-emerald-400" />
                <span className="text-2xl font-bold text-emerald-400 w-8 text-center">{count}</span>
              </div>
            </div>
            <div className="text-sm text-gray-500 space-y-1">
              <div>Image: <span className="text-gray-300">{selectedImage?.path}</span></div>
              <div>Device: <span className="text-gray-300">{selectedDevice}</span></div>
            </div>
          </div>
          {avds.length > 0 && (
            <div className="bg-gray-900 border border-gray-800 rounded-lg p-4">
              <div className="text-sm text-gray-400 mb-2">Existing AVDs ({avds.length})</div>
              <div className="flex flex-wrap gap-1">{avds.map(a => <span key={a} className="text-xs bg-gray-800 px-2 py-0.5 rounded text-gray-400">{a}</span>)}</div>
            </div>
          )}
          <div className="flex gap-3">
            <button onClick={() => setStep('device')} className="px-4 py-2 bg-gray-800 rounded text-sm hover:bg-gray-700 transition">← Back</button>
            <button onClick={handleCreate} className="flex-1 px-4 py-2 bg-emerald-500 text-white rounded text-sm font-medium hover:bg-emerald-400 transition">Create {count} AVD{count > 1 ? 's' : ''}</button>
          </div>
        </div>
      )}

      {step === 'creating' && (
        <div className="text-center py-12">
          <div className="animate-spin w-8 h-8 border-2 border-emerald-400 border-t-transparent rounded-full mx-auto mb-4" />
          <div className="text-gray-400">Creating {count} AVDs...</div>
        </div>
      )}

      {step === 'done' && createResult && (
        <div className="space-y-4">
          <div className="bg-emerald-400/10 border border-emerald-400/30 rounded-lg p-6 text-center">
            <div className="text-emerald-400 text-4xl font-bold mb-2">{createResult.created}</div>
            <div className="text-emerald-300">AVDs created</div>
          </div>
          {createResult.errors?.length > 0 && (
            <div className="bg-red-400/10 border border-red-400/30 rounded-lg p-4">
              {createResult.errors.map((e, i) => <div key={i} className="text-red-300 text-sm">{e}</div>)}
            </div>
          )}
          <button onClick={() => { setStep('image'); setCreateResult(null); }} className="w-full px-4 py-2 bg-gray-800 rounded text-sm hover:bg-gray-700 transition">Create More</button>
        </div>
      )}
    </div>
  );
}
