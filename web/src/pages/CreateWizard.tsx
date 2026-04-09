import { useEffect, useState } from 'react';
import { api, SystemImage } from '../lib/api';
import { DeviceDef, enrichDevices, groupByCategory } from '../lib/devices';

type Step = 'device' | 'image' | 'configure' | 'creating' | 'done';

export function CreateWizard() {
  const [step, setStep] = useState<Step>('device');
  const [images, setImages] = useState<SystemImage[]>([]);
  const [deviceDefs, setDeviceDefs] = useState<DeviceDef[]>([]);
  const [avds, setAvds] = useState<string[]>([]);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);

  const [availableImages, setAvailableImages] = useState<SystemImage[]>([]);
  const [selectedDevice, setSelectedDevice] = useState<DeviceDef | null>(null);
  const [selectedImage, setSelectedImage] = useState<SystemImage | null>(null);
  const [count, setCount] = useState(3);
  const [profileName, setProfileName] = useState('');
  const [activeCategory, setActiveCategory] = useState('phone');
  const [installing, setInstalling] = useState<string | null>(null);
  const [showAll, setShowAll] = useState(false);

  const [createResult, setCreateResult] = useState<{ created: number; errors: string[] } | null>(null);

  useEffect(() => {
    loadData();
  }, []);

  const loadData = async () => {
    try {
      const [i, ai, d, a] = await Promise.all([
        api.systemImages(), api.availableImages(), api.devices(), api.avds()
      ]);
      setImages(i.images || []);
      setAvailableImages(ai.images || []);
      setDeviceDefs(enrichDevices(d.devices || []));
      setAvds((a.avds || []).map(x => x.name));
      setError('');
    } catch (e: any) { setError(e.message); }
    setLoading(false);
  };

  const handleInstall = async (path: string) => {
    setInstalling(path);
    try {
      await api.installImage(path);
      await loadData(); // Refresh lists
    } catch (e: any) { alert(e.message); }
    setInstalling(null);
  };

  const handleCreate = async () => {
    if (!selectedImage || !selectedDevice) return;
    setStep('creating');
    try {
      const result = await api.createAVDs({
        profile_name: profileName,
        device: selectedDevice.id,
        system_image: selectedImage.path,
        count,
      });
      setCreateResult(result);
      setStep('done');
    } catch (e: any) { setError(e.message); setStep('configure'); }
  };

  if (loading) return <div className="text-center py-20 text-gray-500">Loading SDK info...</div>;
  if (error && !['creating', 'done'].includes(step)) return (
    <div className="text-center py-20">
      <div className="text-red-400 text-lg mb-2">Error</div>
      <div className="text-gray-500 text-sm">{error}</div>
    </div>
  );

  const steps = ['device', 'image', 'configure', 'done'];
  const stepLabels = ['Device', 'System Image', 'Configure', 'Done'];
  const currentIdx = steps.indexOf(step === 'creating' ? 'done' : step);
  const groups = groupByCategory(deviceDefs);

  return (
    <div className="max-w-4xl mx-auto">
      {/* Header */}
      <div className="mb-8">
        <h1 className="text-2xl font-bold">Virtual Device Configuration</h1>
        <p className="text-gray-500 mt-1">Select hardware, system image, and configure your emulators.</p>
      </div>

      {/* Step indicator */}
      <div className="flex items-center gap-2 mb-8">
        {steps.map((s, i) => (
          <div key={s} className="flex items-center gap-2 flex-1">
            <div className={`w-7 h-7 rounded-full flex items-center justify-center text-xs font-bold border-2 transition
              ${currentIdx > i ? 'bg-emerald-500 border-emerald-500 text-white' :
                currentIdx === i ? 'border-emerald-400 text-emerald-400' :
                'border-gray-700 text-gray-600'}`}>
              {currentIdx > i ? '✓' : i + 1}
            </div>
            <span className={`text-sm ${currentIdx >= i ? 'text-gray-200' : 'text-gray-600'}`}>{stepLabels[i]}</span>
            {i < steps.length - 1 && <div className={`flex-1 h-px ${currentIdx > i ? 'bg-emerald-500' : 'bg-gray-800'}`} />}
          </div>
        ))}
      </div>

      {/* Step 1: Choose Device */}
      {step === 'device' && (
        <div>
          {/* Category tabs */}
          <div className="flex gap-1 mb-4 overflow-x-auto pb-2">
            {groups.map(g => (
              <button key={g.category} onClick={() => setActiveCategory(g.category)}
                className={`px-3 py-1.5 rounded-md text-sm whitespace-nowrap transition ${
                  activeCategory === g.category ? 'bg-gray-800 text-white' : 'text-gray-500 hover:text-gray-300'
                }`}>
                {g.category.charAt(0).toUpperCase() + g.category.slice(1)} ({g.devices.length})
              </button>
            ))}
          </div>

          {/* Device grid */}
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
            {groups
              .filter(g => g.category === activeCategory)
              .flatMap(g => g.devices)
              .map(dev => (
                <button key={dev.id} onClick={() => { setSelectedDevice(dev); setStep('image'); }}
                  className={`text-left p-4 rounded-lg border transition hover:border-emerald-400/50 ${
                    selectedDevice?.id === dev.id ? 'border-emerald-400 bg-emerald-400/5' : 'border-gray-800 bg-gray-900'
                  }`}>
                  <div className="flex items-start justify-between">
                    <div>
                      <div className="font-medium text-gray-200 text-sm">{dev.name}</div>
                      {dev.screen && <div className="text-xs text-gray-500 mt-1">{dev.screen}</div>}
                      {dev.density && <div className="text-xs text-gray-600">{dev.density}</div>}
                    </div>
                    {dev.popular && <span className="text-xs bg-emerald-400/10 text-emerald-400 px-1.5 py-0.5 rounded">Popular</span>}
                  </div>
                  {dev.year && <div className="text-xs text-gray-600 mt-2">{dev.year}</div>}
                </button>
              ))}
          </div>
        </div>
      )}

      {/* Step 2: Choose System Image — table like Android Studio */}
      {step === 'image' && (() => {
        const installedPaths = new Set(images.map(i => i.path));
        // Filter available to only show not-installed, and recent APIs (21+)
        const notInstalled = availableImages.filter(i => !installedPaths.has(i.path));
        // Group by API level for display
        const apiLevel = (name: string) => {
          const m = name.match(/android-(\d+)/);
          return m ? parseInt(m[1]) : 0;
        };
        const recentNotInstalled = notInstalled
          .filter(i => apiLevel(i.api_name) >= 28)
          .sort((a, b) => apiLevel(b.api_name) - apiLevel(a.api_name));

        return (
          <div className="space-y-4">
            <div className="flex items-center gap-3 mb-2">
              <button onClick={() => setStep('device')} className="text-sm text-gray-500 hover:text-gray-300">← Back</button>
              <div className="text-sm text-gray-400">
                Device: <span className="text-emerald-400">{selectedDevice?.name}</span>
                {selectedDevice?.screen && <span className="text-gray-600"> · {selectedDevice.screen}</span>}
              </div>
            </div>

            <h2 className="text-lg font-semibold">System Image</h2>

            {/* Table */}
            <div className="bg-gray-900 border border-gray-800 rounded-lg overflow-hidden">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-gray-800 text-gray-500 text-xs uppercase tracking-wider">
                    <th className="text-left px-4 py-2.5 font-medium">API Level</th>
                    <th className="text-left px-4 py-2.5 font-medium">Target</th>
                    <th className="text-left px-4 py-2.5 font-medium">Arch</th>
                    <th className="text-right px-4 py-2.5 font-medium">Status</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-800/50">
                  {/* Installed images first */}
                  {images.map(img => (
                    <tr key={img.path}
                      onClick={() => {
                        setSelectedImage(img);
                        let name = img.api_name.replace('android-', 'api').replace(/-/g, '_');
                        if (img.variant.includes('playstore')) name += '_play';
                        setProfileName(name);
                        setStep('configure');
                      }}
                      className={`cursor-pointer transition hover:bg-gray-800/50 ${
                        selectedImage?.path === img.path ? 'bg-emerald-400/5' : ''
                      }`}>
                      <td className="px-4 py-3 font-medium text-gray-200">
                        API {img.api_name.replace('android-', '')}
                      </td>
                      <td className="px-4 py-3 text-gray-400">
                        {img.variant.replace(/_/g, ' ')}
                        {img.variant.includes('playstore') && (
                          <span className="ml-2 text-[10px] bg-blue-400/10 text-blue-400 px-1.5 py-0.5 rounded">Play Store</span>
                        )}
                      </td>
                      <td className="px-4 py-3 text-gray-500 font-mono text-xs">{img.arch}</td>
                      <td className="px-4 py-3 text-right">
                        <span className="text-xs bg-emerald-400/10 text-emerald-400 px-2 py-0.5 rounded">Installed</span>
                      </td>
                    </tr>
                  ))}

                  {/* Available (not installed) */}
                  {(showAll ? recentNotInstalled : recentNotInstalled.slice(0, 5)).map(img => (
                    <tr key={img.path} className="text-gray-500">
                      <td className="px-4 py-3">API {img.api_name.replace('android-', '')}</td>
                      <td className="px-4 py-3 text-gray-600">
                        {img.variant.replace(/_/g, ' ')}
                        {img.variant.includes('playstore') && (
                          <span className="ml-2 text-[10px] bg-blue-400/10 text-blue-300/50 px-1.5 py-0.5 rounded">Play Store</span>
                        )}
                      </td>
                      <td className="px-4 py-3 font-mono text-xs text-gray-600">{img.arch}</td>
                      <td className="px-4 py-3 text-right">
                        <button
                          onClick={(e) => { e.stopPropagation(); handleInstall(img.path); }}
                          disabled={installing === img.path}
                          className="text-xs px-2.5 py-1 bg-blue-500/10 text-blue-400 rounded hover:bg-blue-500/20 transition disabled:opacity-50">
                          {installing === img.path ? 'Installing...' : 'Download'}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>

              {/* Show more / less */}
              {recentNotInstalled.length > 5 && (
                <div className="border-t border-gray-800 px-4 py-2 text-center">
                  <button onClick={() => setShowAll(!showAll)} className="text-xs text-gray-500 hover:text-gray-300 transition">
                    {showAll ? 'Show less' : `Show all ${recentNotInstalled.length} available images`}
                  </button>
                </div>
              )}
            </div>
          </div>
        );
      })()}

      {/* Step 3: Configure */}
      {step === 'configure' && (
        <div className="space-y-6">
          <div className="flex items-center gap-3 mb-4">
            <button onClick={() => setStep('image')} className="text-sm text-gray-500 hover:text-gray-300">← Back</button>
          </div>

          {/* Summary card */}
          <div className="bg-gray-900 border border-gray-800 rounded-lg p-5">
            <h3 className="text-sm font-semibold text-gray-400 uppercase tracking-wider mb-4">Configuration Summary</h3>
            <div className="grid grid-cols-2 gap-4 text-sm">
              <div>
                <span className="text-gray-500">Device:</span>
                <div className="text-gray-200 font-medium">{selectedDevice?.name}</div>
                {selectedDevice?.screen && <div className="text-xs text-gray-500">{selectedDevice.screen}</div>}
              </div>
              <div>
                <span className="text-gray-500">System Image:</span>
                <div className="text-gray-200 font-medium">API {selectedImage?.api_name.replace('android-', '')}</div>
                <div className="text-xs text-gray-500">{selectedImage?.variant}</div>
              </div>
            </div>
          </div>

          {/* Settings */}
          <div className="bg-gray-900 border border-gray-800 rounded-lg p-5 space-y-5">
            <div>
              <label className="block text-sm text-gray-400 mb-2">AVD Profile Name</label>
              <input type="text" value={profileName} onChange={e => setProfileName(e.target.value)}
                className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2.5 text-sm focus:outline-none focus:border-emerald-400 transition" />
              <p className="text-xs text-gray-600 mt-1">AVDs will be named: drizz_{profileName}_0, drizz_{profileName}_1, ...</p>
            </div>

            <div>
              <label className="block text-sm text-gray-400 mb-2">Number of Emulators</label>
              <div className="flex items-center gap-6">
                <input type="range" min={1} max={10} value={count} onChange={e => setCount(+e.target.value)}
                  className="flex-1 accent-emerald-400 h-2" />
                <div className="text-3xl font-bold text-emerald-400 w-10 text-center">{count}</div>
              </div>
              <p className="text-xs text-gray-600 mt-1">Each emulator uses ~2.5GB RAM. Boot on-demand — only runs when sessions need it.</p>
            </div>
          </div>

          {/* Existing AVDs */}
          {avds.length > 0 && (
            <div className="bg-gray-900 border border-gray-800 rounded-lg p-4">
              <div className="text-sm text-gray-400 mb-2">Existing AVDs on this machine ({avds.length})</div>
              <div className="flex flex-wrap gap-1.5">
                {avds.map(a => (
                  <span key={a} className="text-xs bg-gray-800 px-2 py-1 rounded text-gray-400 font-mono">{a}</span>
                ))}
              </div>
            </div>
          )}

          {/* Create button */}
          <button onClick={handleCreate}
            className="w-full py-3 bg-emerald-500 text-white rounded-lg text-sm font-semibold hover:bg-emerald-400 transition">
            Create {count} Emulator{count > 1 ? 's' : ''}
          </button>
        </div>
      )}

      {/* Step 4: Creating */}
      {step === 'creating' && (
        <div className="text-center py-16">
          <div className="animate-spin w-10 h-10 border-3 border-emerald-400 border-t-transparent rounded-full mx-auto mb-6" />
          <div className="text-lg text-gray-300">Creating {count} emulators...</div>
          <div className="text-sm text-gray-500 mt-2">{selectedDevice?.name} · API {selectedImage?.api_name.replace('android-', '')}</div>
        </div>
      )}

      {/* Step 5: Done */}
      {step === 'done' && createResult && (
        <div className="space-y-6">
          <div className="bg-emerald-400/10 border border-emerald-400/30 rounded-lg p-8 text-center">
            <div className="text-5xl font-bold text-emerald-400 mb-2">{createResult.created}</div>
            <div className="text-emerald-300 text-lg">Emulators Created</div>
            <div className="text-gray-500 text-sm mt-2">{selectedDevice?.name} · API {selectedImage?.api_name.replace('android-', '')}</div>
          </div>

          {createResult.errors?.length > 0 && (
            <div className="bg-red-400/10 border border-red-400/30 rounded-lg p-4">
              <div className="text-sm text-red-400 font-medium mb-2">Errors:</div>
              {createResult.errors.map((e, i) => <div key={i} className="text-sm text-red-300">{e}</div>)}
            </div>
          )}

          <div className="bg-gray-900 border border-gray-800 rounded-lg p-5 text-center">
            <div className="text-gray-400 text-sm">Emulators boot on-demand when you create sessions.</div>
            <div className="text-gray-500 text-xs mt-1">Go to Dashboard to start one, or create a session.</div>
          </div>

          <div className="flex gap-3">
            <button onClick={() => { setStep('device'); setCreateResult(null); }}
              className="flex-1 py-2.5 bg-gray-800 rounded-lg text-sm hover:bg-gray-700 transition">Create More</button>
            <a href="/" className="flex-1 py-2.5 bg-emerald-500 text-white rounded-lg text-sm font-medium text-center hover:bg-emerald-400 transition">Go to Dashboard</a>
          </div>
        </div>
      )}
    </div>
  );
}
