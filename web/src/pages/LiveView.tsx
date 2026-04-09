import { useEffect, useRef, useState, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { api, DeviceInstance } from '../lib/api';

export function LiveView() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const inputWsRef = useRef<WebSocket | null>(null);
  const [connected, setConnected] = useState(false);
  const [instance, setInstance] = useState<DeviceInstance | null>(null);
  const [fps, setFps] = useState(0);
  const frameCount = useRef(0);

  // Load instance info
  useEffect(() => {
    api.pool().then(p => {
      const inst = p.instances.find(i => i.id === id || i.session_id === id);
      if (inst) setInstance(inst);
    });
  }, [id]);

  // Screen WebSocket
  useEffect(() => {
    if (!id) return;

    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${proto}//${window.location.host}/api/v1/sessions/${id}/screen`;
    const ws = new WebSocket(wsUrl);
    ws.binaryType = 'arraybuffer';
    wsRef.current = ws;

    ws.onopen = () => setConnected(true);
    ws.onclose = () => setConnected(false);
    ws.onerror = () => setConnected(false);

    ws.onmessage = (event) => {
      frameCount.current++;
      const blob = new Blob([event.data], { type: 'image/png' });
      const url = URL.createObjectURL(blob);
      const img = new Image();
      img.onload = () => {
        const canvas = canvasRef.current;
        if (!canvas) return;
        canvas.width = img.width;
        canvas.height = img.height;
        const ctx = canvas.getContext('2d');
        if (ctx) ctx.drawImage(img, 0, 0);
        URL.revokeObjectURL(url);
      };
      img.src = url;
    };

    return () => ws.close();
  }, [id]);

  // Input WebSocket
  useEffect(() => {
    if (!id) return;

    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${proto}//${window.location.host}/api/v1/sessions/${id}/input`;
    const ws = new WebSocket(wsUrl);
    inputWsRef.current = ws;

    return () => ws.close();
  }, [id]);

  // FPS counter
  useEffect(() => {
    const interval = setInterval(() => {
      setFps(frameCount.current);
      frameCount.current = 0;
    }, 1000);
    return () => clearInterval(interval);
  }, []);

  // Input handlers
  const sendInput = useCallback((cmd: string) => {
    if (inputWsRef.current?.readyState === WebSocket.OPEN) {
      inputWsRef.current.send(cmd);
    }
  }, []);

  const handleCanvasClick = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const rect = canvas.getBoundingClientRect();
    const scaleX = canvas.width / rect.width;
    const scaleY = canvas.height / rect.height;
    const x = Math.round((e.clientX - rect.left) * scaleX);
    const y = Math.round((e.clientY - rect.top) * scaleY);
    sendInput(`tap ${x} ${y}`);
  }, [sendInput]);

  const [adbCmd, setAdbCmd] = useState('');
  const [adbOutput, setAdbOutput] = useState('');
  const [activeGPS, setActiveGPS] = useState('');
  const [activeNetwork, setActiveNetwork] = useState('');
  const [activeBattery, setActiveBattery] = useState(0);
  const [activeRotation, setActiveRotation] = useState(0);
  const [activeDark, setActiveDark] = useState<boolean | null>(null);
  const [activeLocale, setActiveLocale] = useState('');

  return (
    <div className="flex gap-4">
      {/* Left: Screen — fixed width */}
      <div className="w-[260px] flex-shrink-0">
        <div className="flex items-center gap-3 mb-2">
          <button onClick={() => navigate('/')} className="text-xs text-gray-500 hover:text-gray-300">← Back</button>
          <span className="text-xs font-mono text-purple-400">{instance?.device_name || id}</span>
          <div className={`w-1.5 h-1.5 rounded-full ${connected ? 'bg-emerald-400' : 'bg-red-400'}`} />
          <span className="text-[10px] text-gray-600">{connected ? `${fps} fps` : 'disconnected'}</span>
        </div>
        <div className="bg-gray-900 border border-gray-800 rounded-lg p-1 inline-block">
          {!connected ? (
            <div className="w-[240px] h-[533px] flex items-center justify-center text-gray-500">
              <div className="animate-spin w-5 h-5 border-2 border-gray-600 border-t-emerald-400 rounded-full" />
            </div>
          ) : (
            <canvas ref={canvasRef} onClick={handleCanvasClick}
              className="w-[240px] h-[533px] cursor-crosshair rounded" style={{ imageRendering: 'auto' }} />
          )}
        </div>
        <div className="flex gap-1 mt-2 justify-center flex-wrap">
          <button onClick={() => sendInput('back')} className="px-3 py-1 bg-gray-800 rounded text-[10px] hover:bg-gray-700">◀ Back</button>
          <button onClick={() => sendInput('home')} className="px-3 py-1 bg-gray-800 rounded text-[10px] hover:bg-gray-700">● Home</button>
          <button onClick={() => sendInput('recent')} className="px-3 py-1 bg-gray-800 rounded text-[10px] hover:bg-gray-700">■ Recent</button>
          <button onClick={async () => {
            if (!id) return;
            const r = await api.execADB(id, "dumpsys activity recents | grep realActivity");
            const lines = (r.output||'').split('\n');
            const pkgs = new Set<string>();
            for (const line of lines) {
              const m = line.match(/realActivity=\{?([^/}]+)/);
              if (m) pkgs.add(m[1].trim());
            }
            const skip = ['com.google.android.apps.nexuslauncher','com.android.launcher3'];
            for (const pkg of pkgs) { if (!skip.includes(pkg)) await api.execADB(id, `am force-stop ${pkg}`); }
          }} className="px-3 py-1 bg-red-900/50 text-red-400 rounded text-[10px] hover:bg-red-900/70">✕ Close All</button>
        </div>
      </div>

      {/* Right: Controls */}
      <div className="flex-1 space-y-3 overflow-y-auto max-h-[600px]">
        {/* GPS */}
        <Panel title="GPS">
          <div className="flex gap-2 flex-wrap mb-2">
            {[
              { label: 'San Francisco', lat: 37.7749, lng: -122.4194 },
              { label: 'New York', lat: 40.7128, lng: -74.006 },
              { label: 'London', lat: 51.5074, lng: -0.1278 },
              { label: 'Tokyo', lat: 35.6762, lng: 139.6503 },
              { label: 'Mumbai', lat: 19.076, lng: 72.8777 },
              { label: 'Bangalore', lat: 12.9716, lng: 77.5946 },
            ].map(loc => (
              <Chip key={loc.label} active={activeGPS === loc.label} onClick={() => { if (id) { api.setGPS(id, loc.lat, loc.lng); setActiveGPS(loc.label); } }}>{loc.label}</Chip>
            ))}
          </div>
          <div className="flex gap-2 items-center">
            <input type="text" placeholder="lat" id="gps-lat" className="w-20 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] font-mono focus:outline-none focus:border-emerald-400" />
            <input type="text" placeholder="lng" id="gps-lng" className="w-20 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] font-mono focus:outline-none focus:border-emerald-400" />
            <button onClick={() => {
              const lat = parseFloat((document.getElementById('gps-lat') as HTMLInputElement)?.value);
              const lng = parseFloat((document.getElementById('gps-lng') as HTMLInputElement)?.value);
              if (id && !isNaN(lat) && !isNaN(lng)) { api.setGPS(id, lat, lng); setActiveGPS(`${lat},${lng}`); }
            }} className="px-2 py-1 bg-emerald-500/20 text-emerald-400 border border-emerald-500/50 rounded text-[10px] hover:bg-emerald-500/30">Set</button>
          </div>
        </Panel>

        {/* Network + Battery side by side */}
        <div className="grid grid-cols-2 gap-3">
          <Panel title="Network">
            <div className="flex gap-1.5 flex-wrap">
              {['2g', '3g', '4g', '5g', 'wifi_slow', 'wifi_fast', 'offline'].map(p => (
                <Chip key={p} active={activeNetwork === p} onClick={() => { if (id) { api.setNetwork(id, p); setActiveNetwork(p); } }}>{p}</Chip>
              ))}
            </div>
          </Panel>
          <Panel title="Battery">
            <div className="flex gap-1.5 flex-wrap">
              {[100, 75, 50, 25, 10, 5].map(l => (
                <Chip key={l} active={activeBattery === l} onClick={() => { if (id) { api.setBattery(id, l, l > 20 ? 'ac' : 'none'); setActiveBattery(l); } }}>{l}%</Chip>
              ))}
            </div>
          </Panel>
        </div>

        {/* Orientation + Appearance + Locale side by side */}
        <div className="grid grid-cols-3 gap-3">
          <Panel title="Orientation">
            <div className="flex gap-1.5 flex-wrap">
              {[
                { label: '↑', r: 0 },
                { label: '←', r: 1 },
                { label: '↓', r: 2 },
                { label: '→', r: 3 },
              ].map(o => (
                <Chip key={o.r} active={activeRotation === o.r} onClick={() => { if (id) { api.setOrientation(id, o.r); setActiveRotation(o.r); } }}>{o.label}</Chip>
              ))}
            </div>
          </Panel>
          <Panel title="Appearance">
            <div className="flex gap-1.5">
              <Chip active={activeDark === true} onClick={() => { if (id) { api.setDarkMode(id, true); setActiveDark(true); } }}>Dark</Chip>
              <Chip active={activeDark === false} onClick={() => { if (id) { api.setDarkMode(id, false); setActiveDark(false); } }}>Light</Chip>
            </div>
          </Panel>
          <Panel title="Locale">
            <select value={activeLocale} onChange={e => { if (id) { api.setLocale(id, e.target.value); setActiveLocale(e.target.value); } }}
              className="w-full bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] font-mono focus:outline-none focus:border-emerald-400">
              <option value="">Select...</option>
              {['en-US','en-GB','es-ES','es-MX','pt-BR','fr-FR','de-DE','it-IT','nl-NL','ru-RU','pl-PL','tr-TR',
                'ja-JP','ko-KR','zh-CN','zh-TW','hi-IN','bn-IN','ta-IN','te-IN','mr-IN','gu-IN','kn-IN','ml-IN',
                'ar-SA','he-IL','th-TH','vi-VN','id-ID','ms-MY','fil-PH','sv-SE','da-DK','nb-NO','fi-FI',
                'uk-UA','cs-CZ','ro-RO','hu-HU','el-GR','bg-BG','hr-HR','sk-SK','sl-SI',
                'sw-KE','am-ET','af-ZA'].map(l => (
                <option key={l} value={l}>{l}</option>
              ))}
            </select>
          </Panel>
        </div>

        {/* Deeplink */}
        <Panel title="Deep Link">
          <div className="flex gap-2">
            <input type="text" placeholder="https://..." className="flex-1 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] focus:outline-none focus:border-emerald-400"
              onKeyDown={e => { if (e.key === 'Enter' && id) api.openDeeplink(id, (e.target as HTMLInputElement).value); }} />
          </div>
        </Panel>

        {/* ADB Shell */}
        <Panel title="ADB Shell">
          <div className="flex gap-2">
            <input type="text" placeholder="shell command..." value={adbCmd} onChange={e => setAdbCmd(e.target.value)}
              className="flex-1 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] font-mono focus:outline-none focus:border-emerald-400"
              onKeyDown={async e => {
                if (e.key === 'Enter' && id && adbCmd) {
                  const r = await api.execADB(id, adbCmd);
                  setAdbOutput(r.output || r.error || '');
                  setAdbCmd('');
                }
              }} />
          </div>
          {adbOutput && <pre className="mt-1 text-[9px] text-gray-500 font-mono bg-gray-950 p-2 rounded max-h-24 overflow-auto">{adbOutput}</pre>}
        </Panel>

        {/* Logcat */}
        <LogcatPanel instanceId={id || ''} />
      </div>
    </div>
  );
}

function LogcatPanel({ instanceId }: { instanceId: string }) {
  const logRef = useRef<HTMLPreElement>(null);
  const [lines, setLines] = useState<string[]>([]);
  const [paused, setPaused] = useState(false);

  useEffect(() => {
    if (!instanceId) return;
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${proto}//${window.location.host}/api/v1/sessions/${instanceId}/logcat`);

    ws.onmessage = (e) => {
      if (paused) return;
      const newLines = e.data.split('\n').filter((l: string) => l.trim());
      setLines(prev => [...prev.slice(-200), ...newLines]);
      if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight;
    };

    return () => ws.close();
  }, [instanceId, paused]);

  return (
    <div className="bg-gray-900 border border-gray-800 rounded-lg">
      <div className="flex items-center justify-between px-3 py-1.5 border-b border-gray-800">
        <span className="text-[10px] text-gray-500 uppercase tracking-wider">Logcat</span>
        <div className="flex gap-2">
          <button onClick={() => setPaused(!paused)}
            className={`text-[10px] px-2 py-0.5 rounded ${paused ? 'bg-yellow-500/20 text-yellow-400' : 'bg-gray-800 text-gray-400 hover:bg-gray-700'}`}>
            {paused ? 'Resume' : 'Pause'}
          </button>
          <button onClick={() => setLines([])} className="text-[10px] px-2 py-0.5 bg-gray-800 text-gray-400 rounded hover:bg-gray-700">Clear</button>
        </div>
      </div>
      <pre ref={logRef} className="p-2 text-[8px] font-mono text-gray-500 h-[150px] overflow-auto leading-tight whitespace-pre-wrap">
        {lines.length === 0 ? 'Waiting for logs...' : lines.join('\n')}
      </pre>
    </div>
  );
}

function Chip({ active, mono, onClick, children }: { active?: boolean; mono?: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button onClick={onClick}
      className={`px-2 py-1 rounded text-[10px] transition ${mono ? 'font-mono' : ''} ${
        active
          ? 'bg-emerald-500/20 text-emerald-400 border border-emerald-500/50'
          : 'bg-gray-800 text-gray-300 border border-transparent hover:bg-gray-700'
      }`}>
      {children}
    </button>
  );
}

function Panel({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="bg-gray-900 border border-gray-800 rounded-lg p-3">
      <div className="text-[10px] text-gray-500 uppercase tracking-wider mb-2">{title}</div>
      {children}
    </div>
  );
}
