import { useEffect, useRef, useState, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { api, DeviceInstance } from '../lib/api';
import JMuxer from 'jmuxer';

type Tab = 'device' | 'input' | 'apps' | 'capture' | 'debug';

export function LiveView() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const inputWsRef = useRef<WebSocket | null>(null);
  const [connected, setConnected] = useState(false);
  const [inputConnected, setInputConnected] = useState(false);
  const [instance, setInstance] = useState<DeviceInstance | null>(null);
  const [fps, setFps] = useState(0);
  const frameCount = useRef(0);
  const [activeTab, setActiveTab] = useState<Tab>('device');

  // State for controls
  const [activeGPS, setActiveGPS] = useState('');
  const [activeNetwork, setActiveNetwork] = useState('');
  const [activeBattery, setActiveBattery] = useState(0);
  const [activeRotation, setActiveRotation] = useState(0);
  const [activeDark, setActiveDark] = useState<boolean | null>(null);
  const [activeLocale, setActiveLocale] = useState('');
  const [recording, setRecording] = useState(false);
  const [harCapturing, setHarCapturing] = useState(false);
  const [adbCmd, setAdbCmd] = useState('');
  const [adbOutput, setAdbOutput] = useState('');
  const [deviceInfo, setDeviceInfo] = useState<any>(null);

  useEffect(() => { api.pool().then(p => { const inst = p.instances.find((i: DeviceInstance) => i.id === id || i.session_id === id); if (inst) setInstance(inst); }); }, [id]);

  // Screen WebSocket — supports H.264 (via jmuxer) and PNG fallback
  const videoRef = useRef<HTMLVideoElement>(null);
  const jmuxerRef = useRef<JMuxer | null>(null);
  const [codec, setCodec] = useState<'h264' | 'png' | null>(null);

  useEffect(() => {
    if (!id) return;
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${proto}//${window.location.host}/api/v1/sessions/${id}/screen`);
    ws.binaryType = 'arraybuffer';
    wsRef.current = ws;
    ws.onopen = () => setConnected(true);
    ws.onclose = () => setConnected(false);

    let detectedCodec = '';

    ws.onmessage = (event) => {
      if (typeof event.data === 'string') {
        try {
          const info = JSON.parse(event.data);
          detectedCodec = info.codec;
          setCodec(info.codec as any);
        } catch {}
        return;
      }

      frameCount.current++;

      if (detectedCodec === 'h264') {
        // Feed to jmuxer if ready
        if (jmuxerRef.current) {
          jmuxerRef.current.feed({ video: new Uint8Array(event.data) });
        }
      } else {
        // PNG fallback
        const blob = new Blob([event.data], { type: 'image/png' });
        const url = URL.createObjectURL(blob);
        const img = new Image();
        img.onload = () => { const c = canvasRef.current; if (!c) return; c.width = img.width; c.height = img.height; c.getContext('2d')?.drawImage(img, 0, 0); URL.revokeObjectURL(url); };
        img.src = url;
      }
    };

    return () => { ws.close(); };
  }, [id]);

  // Init jmuxer after codec is detected and video element is rendered
  useEffect(() => {
    if (codec !== 'h264') return;
    // Small delay to let React render the <video> element
    const timer = setTimeout(() => {
      if (videoRef.current && !jmuxerRef.current) {
        jmuxerRef.current = new JMuxer({
          node: videoRef.current,
          mode: 'video',
          fps: 30,
          flushingTime: 0,
          debug: false,
        });
        videoRef.current.play();
        console.log('jmuxer initialized');
      }
    }, 100);
    return () => { clearTimeout(timer); jmuxerRef.current?.destroy(); jmuxerRef.current = null; };
  }, [codec]);

  // Input WebSocket
  useEffect(() => {
    if (!id) return;
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${proto}//${window.location.host}/api/v1/sessions/${id}/input`);
    inputWsRef.current = ws;
    ws.onopen = () => setInputConnected(true);
    ws.onclose = () => setInputConnected(false);
    return () => ws.close();
  }, [id]);

  useEffect(() => { const i = setInterval(() => { setFps(frameCount.current); frameCount.current = 0; }, 1000); return () => clearInterval(i); }, []);

  const sendInput = useCallback((cmd: string) => { if (inputWsRef.current?.readyState === WebSocket.OPEN) inputWsRef.current.send(cmd); }, []);

  // Gestures
  const dragStart = useRef<{ x: number; y: number; time: number } | null>(null);
  const canvasCoords = useCallback((e: React.MouseEvent<HTMLCanvasElement | HTMLVideoElement>) => {
    const el = (codec === 'h264' ? videoRef.current : canvasRef.current) as HTMLElement;
    if (!el) return { x: 0, y: 0 };
    const r = el.getBoundingClientRect();
    // Map display coords to device coords (720x1600 for scrcpy, 1080x2400 for screencap)
    const deviceW = codec === 'h264' ? 720 : 1080;
    const deviceH = codec === 'h264' ? 1600 : 2400;
    return { x: Math.round((e.clientX - r.left) / r.width * deviceW), y: Math.round((e.clientY - r.top) / r.height * deviceH) };
  }, [codec]);
  const handleMouseDown = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => { dragStart.current = { ...canvasCoords(e), time: Date.now() }; }, [canvasCoords]);
  const handleMouseUp = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
    if (!dragStart.current) return;
    const s = dragStart.current; const end = canvasCoords(e); dragStart.current = null;
    const dist = Math.sqrt((end.x-s.x)**2 + (end.y-s.y)**2);
    if (dist < 50) sendInput(`tap ${s.x} ${s.y}`);
    else sendInput(`swipe ${s.x} ${s.y} ${end.x} ${end.y} ${Math.max(150, Math.min(Date.now()-s.time, 2000))}`);
  }, [canvasCoords, sendInput]);
  const handleMouseLeave = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
    if (!dragStart.current) return;
    const s = dragStart.current; const end = canvasCoords(e); dragStart.current = null;
    sendInput(`swipe ${s.x} ${s.y} ${end.x} ${end.y} ${Math.max(150, Math.min(Date.now()-s.time, 2000))}`);
  }, [canvasCoords, sendInput]);

  // Keyboard
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement || e.target instanceof HTMLSelectElement) return;
      e.preventDefault();
      const keys: Record<string, string> = { Backspace: 'key 67', Enter: 'key 66', Escape: 'back', Tab: 'key 61', ArrowUp: 'key 19', ArrowDown: 'key 20', ArrowLeft: 'key 21', ArrowRight: 'key 22' };
      if (keys[e.key]) sendInput(keys[e.key]);
      else if (e.key.length === 1) sendInput(`text ${e.key}`);
    };
    window.addEventListener('keydown', h);
    return () => window.removeEventListener('keydown', h);
  }, [sendInput]);

  // Helpers
  const chip = (label: string, active: boolean, onClick: () => void) => (
    <button key={label} onClick={onClick} className={`px-2 py-1 rounded text-[10px] transition ${active ? 'bg-emerald-500/20 text-emerald-400 border border-emerald-500/50' : 'bg-gray-800 text-gray-300 border border-transparent hover:bg-gray-700'}`}>{label}</button>
  );

  const tabs: { id: Tab; label: string }[] = [
    { id: 'device', label: 'Device' },
    { id: 'input', label: 'Input' },
    { id: 'apps', label: 'Apps' },
    { id: 'capture', label: 'Capture' },
    { id: 'debug', label: 'Debug' },
  ];

  return (
    <div className="flex gap-4">
      {/* Left: Screen */}
      <div className="w-[260px] flex-shrink-0">
        <div className="flex items-center gap-2 mb-2">
          <button onClick={() => navigate('/')} className="text-[10px] text-gray-500 hover:text-gray-300">← Back</button>
          <span className="text-xs font-mono text-purple-400">{instance?.device_name || id}</span>
          <div className={`w-1.5 h-1.5 rounded-full ${connected ? 'bg-emerald-400' : 'bg-red-400'}`} />
          <span className="text-[9px] text-gray-600">{connected ? `${fps}fps` : 'off'}{inputConnected ? '' : ' · no input'}</span>
        </div>
        <div className="bg-gray-900 border border-gray-800 rounded-lg p-1 inline-block">
          {!connected ? (
            <div className="w-[240px] h-[533px] flex items-center justify-center"><div className="animate-spin w-5 h-5 border-2 border-gray-600 border-t-emerald-400 rounded-full" /></div>
          ) : codec === 'h264' ? (
            <video ref={videoRef} autoPlay muted playsInline
              onMouseDown={handleMouseDown as any} onMouseUp={handleMouseUp as any} onMouseLeave={handleMouseLeave as any}
              className="w-[240px] h-[533px] cursor-crosshair rounded select-none object-cover" />
          ) : (
            <canvas ref={canvasRef} onMouseDown={handleMouseDown} onMouseUp={handleMouseUp} onMouseLeave={handleMouseLeave}
              className="w-[240px] h-[533px] cursor-crosshair rounded select-none" style={{ imageRendering: 'auto' }} />
          )}
        </div>
        <div className="flex gap-1 mt-2 justify-center flex-wrap">
          <NavBtn onClick={() => sendInput('back')}>◀ Back</NavBtn>
          <NavBtn onClick={() => sendInput('home')}>● Home</NavBtn>
          <NavBtn onClick={() => sendInput('recent')}>■ Recent</NavBtn>
          <NavBtn onClick={() => sendInput('key 26')}>⏻</NavBtn>
          <button onClick={async () => { if (!id) return; const r = await api.execADB(id, "dumpsys activity recents | grep realActivity"); const lines = (r.output||'').split('\n'); const pkgs = new Set<string>(); for (const l of lines) { const m = l.match(/realActivity=\{?([^/}]+)/); if (m) pkgs.add(m[1].trim()); } const skip = ['com.google.android.apps.nexuslauncher','com.android.launcher3']; for (const pkg of pkgs) { if (!skip.includes(pkg)) await api.execADB(id, `am force-stop ${pkg}`); } }}
            className="px-2 py-1 bg-red-900/50 text-red-400 rounded text-[10px] hover:bg-red-900/70">✕ Close All</button>
        </div>
      </div>

      {/* Right: Tabbed Controls */}
      <div className="flex-1 min-w-0">
        {/* Tab bar */}
        <div className="flex gap-1 mb-3 border-b border-gray-800 pb-px">
          {tabs.map(t => (
            <button key={t.id} onClick={() => setActiveTab(t.id)}
              className={`px-3 py-1.5 text-[10px] font-medium border-b-2 transition ${activeTab === t.id ? 'border-emerald-400 text-emerald-400' : 'border-transparent text-gray-500 hover:text-gray-300'}`}>{t.label}</button>
          ))}
        </div>

        <div className="space-y-3 overflow-y-auto max-h-[550px]">
          {/* ===== DEVICE TAB ===== */}
          {activeTab === 'device' && <>
            <Panel title="GPS">
              <div className="flex gap-1.5 flex-wrap mb-2">
                {[{ l: 'San Francisco', lat: 37.7749, lng: -122.4194 }, { l: 'New York', lat: 40.7128, lng: -74.006 }, { l: 'London', lat: 51.5074, lng: -0.1278 }, { l: 'Tokyo', lat: 35.6762, lng: 139.6503 }, { l: 'Mumbai', lat: 19.076, lng: 72.8777 }, { l: 'Bangalore', lat: 12.9716, lng: 77.5946 }].map(loc => chip(loc.l, activeGPS === loc.l, () => { if (id) { api.setGPS(id, loc.lat, loc.lng); setActiveGPS(loc.l); } }))}
              </div>
              <div className="flex gap-2 items-center">
                <input type="text" placeholder="lat" id="gps-lat" className="w-20 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] font-mono focus:outline-none focus:border-emerald-400" />
                <input type="text" placeholder="lng" id="gps-lng" className="w-20 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] font-mono focus:outline-none focus:border-emerald-400" />
                <button onClick={() => { const lat = parseFloat((document.getElementById('gps-lat') as HTMLInputElement)?.value); const lng = parseFloat((document.getElementById('gps-lng') as HTMLInputElement)?.value); if (id && !isNaN(lat) && !isNaN(lng)) { api.setGPS(id, lat, lng); setActiveGPS(`${lat},${lng}`); } }} className="px-2 py-1 bg-emerald-500/20 text-emerald-400 border border-emerald-500/50 rounded text-[10px]">Set</button>
              </div>
            </Panel>
            <div className="grid grid-cols-2 gap-3">
              <Panel title="Network">
                <div className="flex gap-1 flex-wrap">{['2g','3g','4g','5g','wifi_slow','wifi_fast','offline'].map(p => chip(p, activeNetwork===p, () => { if(id){api.setNetwork(id,p);setActiveNetwork(p)} }))}</div>
              </Panel>
              <Panel title="Battery">
                <div className="flex gap-1 flex-wrap">{[100,75,50,25,10,5].map(l => chip(`${l}%`, activeBattery===l, () => { if(id){api.setBattery(id,l,l>20?'ac':'none');setActiveBattery(l)} }))}</div>
              </Panel>
            </div>
            <div className="grid grid-cols-3 gap-3">
              <Panel title="Orientation">
                <div className="flex gap-1 flex-wrap">{[{l:'↑',r:0},{l:'←',r:1},{l:'↓',r:2},{l:'→',r:3}].map(o => chip(o.l, activeRotation===o.r, () => { if(id){api.setOrientation(id,o.r);setActiveRotation(o.r)} }))}</div>
              </Panel>
              <Panel title="Appearance">
                <div className="flex gap-1">
                  {chip('Dark', activeDark===true, () => { if(id){api.setDarkMode(id,true);setActiveDark(true)} })}
                  {chip('Light', activeDark===false, () => { if(id){api.setDarkMode(id,false);setActiveDark(false)} })}
                </div>
              </Panel>
              <Panel title="Locale">
                <select value={activeLocale} onChange={e => { if(id){api.setLocale(id,e.target.value);setActiveLocale(e.target.value)} }}
                  className="w-full bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] font-mono focus:outline-none focus:border-emerald-400">
                  <option value="">Select...</option>
                  {['en-US','ja-JP','hi-IN','de-DE','fr-FR','zh-CN','ar-SA','ko-KR','es-ES','pt-BR','ru-RU','it-IT','nl-NL','sv-SE','th-TH','vi-VN','id-ID','ms-MY','tr-TR'].map(l => <option key={l} value={l}>{l}</option>)}
                </select>
              </Panel>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <Panel title="Timezone">
                <select onChange={e => { if(id) api.execADB(id, `setprop persist.sys.timezone ${e.target.value}`) }}
                  className="w-full bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] font-mono focus:outline-none focus:border-emerald-400">
                  <option value="">Select...</option>
                  {['America/New_York','America/Chicago','America/Denver','America/Los_Angeles','Europe/London','Europe/Paris','Europe/Berlin','Asia/Tokyo','Asia/Shanghai','Asia/Kolkata','Asia/Dubai','Australia/Sydney','Pacific/Auckland'].map(t => <option key={t} value={t}>{t}</option>)}
                </select>
              </Panel>
              <Panel title="Brightness">
                <input type="range" min={0} max={255} defaultValue={128} onChange={e => { if(id) api.execADB(id, `settings put system screen_brightness ${e.target.value}`) }}
                  className="w-full accent-emerald-400 h-1.5" />
              </Panel>
            </div>
            <div className="grid grid-cols-3 gap-3">
              <Panel title="Font Scale">
                <div className="flex gap-1 flex-wrap">{[{l:'S',v:0.85},{l:'M',v:1.0},{l:'L',v:1.3},{l:'XL',v:1.5}].map(f => (
                  <button key={f.l} onClick={() => { if(id) api.execADB(id, `settings put system font_scale ${f.v}`) }} className="px-2 py-1 bg-gray-800 rounded text-[10px] hover:bg-gray-700">{f.l}</button>
                ))}</div>
              </Panel>
              <Panel title="WiFi">
                <div className="flex gap-1">
                  <button onClick={() => { if(id) api.execADB(id, 'svc wifi enable') }} className="px-2 py-1 bg-emerald-800/50 text-emerald-400 rounded text-[10px]">On</button>
                  <button onClick={() => { if(id) api.execADB(id, 'svc wifi disable') }} className="px-2 py-1 bg-red-800/50 text-red-400 rounded text-[10px]">Off</button>
                </div>
              </Panel>
              <Panel title="Animations">
                <div className="flex gap-1">
                  <button onClick={() => { if(id) { api.execADB(id, 'settings put global window_animation_scale 0'); api.execADB(id, 'settings put global transition_animation_scale 0'); api.execADB(id, 'settings put global animator_duration_scale 0'); } }} className="px-2 py-1 bg-red-800/50 text-red-400 rounded text-[10px]">Off</button>
                  <button onClick={() => { if(id) { api.execADB(id, 'settings put global window_animation_scale 1'); api.execADB(id, 'settings put global transition_animation_scale 1'); api.execADB(id, 'settings put global animator_duration_scale 1'); } }} className="px-2 py-1 bg-emerald-800/50 text-emerald-400 rounded text-[10px]">On</button>
                </div>
              </Panel>
            </div>
            <Panel title="Accessibility">
              <div className="flex gap-1">
                <button onClick={() => { if(id) { api.execADB(id, 'settings put secure enabled_accessibility_services com.google.android.marvin.talkback/com.google.android.marvin.talkback.TalkBackService'); api.execADB(id, 'settings put secure accessibility_enabled 1'); } }} className="px-2 py-1 bg-gray-800 rounded text-[10px] hover:bg-gray-700">TalkBack On</button>
                <button onClick={() => { if(id) { api.execADB(id, 'settings put secure enabled_accessibility_services ""'); api.execADB(id, 'settings put secure accessibility_enabled 0'); } }} className="px-2 py-1 bg-gray-800 rounded text-[10px] hover:bg-gray-700">TalkBack Off</button>
              </div>
            </Panel>
          </>}

          {/* ===== INPUT TAB ===== */}
          {activeTab === 'input' && <>
            <Panel title="D-Pad">
              <div className="flex justify-center">
                <div className="grid grid-cols-3 gap-1 w-fit">
                  <div /><NavBtn onClick={() => sendInput('key 19')}>▲</NavBtn><div />
                  <NavBtn onClick={() => sendInput('key 21')}>◀</NavBtn>
                  <NavBtn onClick={() => sendInput('key 23')}>●</NavBtn>
                  <NavBtn onClick={() => sendInput('key 22')}>▶</NavBtn>
                  <div /><NavBtn onClick={() => sendInput('key 20')}>▼</NavBtn><div />
                </div>
              </div>
            </Panel>
            <div className="grid grid-cols-2 gap-3">
              <Panel title="Volume">
                <div className="flex gap-1">{[{l:'🔈 Down',k:'key 25'},{l:'🔊 Up',k:'key 24'},{l:'🔇 Mute',k:'key 164'}].map(v => (
                  <button key={v.l} onClick={() => sendInput(v.k)} className="px-2 py-1 bg-gray-800 rounded text-[10px] hover:bg-gray-700">{v.l}</button>
                ))}</div>
              </Panel>
              <Panel title="Lock/Unlock">
                <div className="flex gap-1">
                  <button onClick={() => sendInput('key 26')} className="px-2 py-1 bg-gray-800 rounded text-[10px] hover:bg-gray-700">Lock</button>
                  <button onClick={() => { sendInput('key 224'); setTimeout(() => { if(id) api.execADB(id, 'input swipe 540 1800 540 800 300'); }, 500); }} className="px-2 py-1 bg-gray-800 rounded text-[10px] hover:bg-gray-700">Unlock</button>
                </div>
              </Panel>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <Panel title="Biometric">
                <div className="flex gap-1">
                  <button onClick={() => { if(id) api.execADB(id, 'emu finger touch 1') }} className="px-2 py-1 bg-emerald-800/50 text-emerald-400 rounded text-[10px]">Touch OK</button>
                  <button onClick={() => { if(id) api.execADB(id, 'emu finger touch bad') }} className="px-2 py-1 bg-red-800/50 text-red-400 rounded text-[10px]">Fail</button>
                </div>
              </Panel>
              <Panel title="Shake">
                <button onClick={() => { if(id) { api.execADB(id, 'emu sensor set acceleration 0:15:0'); setTimeout(() => { if(id) api.execADB(id, 'emu sensor set acceleration 0:0:9.8'); }, 200); } }} className="px-2 py-1 bg-gray-800 rounded text-[10px] hover:bg-gray-700">Shake Device</button>
              </Panel>
            </div>
            <Panel title="Phone Call">
              <div className="flex gap-1.5">
                <input type="text" placeholder="+1234567890" id="phone-num" className="flex-1 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] font-mono focus:outline-none focus:border-emerald-400" />
                <button onClick={() => { const n=(document.getElementById('phone-num') as HTMLInputElement)?.value; if(id&&n) api.execADB(id,`emu gsm call ${n}`); }} className="px-2 py-1 bg-emerald-500/20 text-emerald-400 rounded text-[10px]">Call</button>
              </div>
            </Panel>
            <Panel title="SMS">
              <div className="flex gap-1.5">
                <input type="text" placeholder="Message..." id="sms-text" className="flex-1 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] focus:outline-none focus:border-emerald-400" />
                <button onClick={() => { const t=(document.getElementById('sms-text') as HTMLInputElement)?.value; if(id&&t) api.execADB(id,`emu sms send 1234567890 ${t}`); }} className="px-2 py-1 bg-blue-500/20 text-blue-400 rounded text-[10px]">Send</button>
              </div>
            </Panel>
            <Panel title="Clipboard">
              <div className="flex gap-1.5">
                <input type="text" placeholder="Text to set..." id="clip-text" className="flex-1 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] focus:outline-none focus:border-emerald-400" />
                <button onClick={() => { const t=(document.getElementById('clip-text') as HTMLInputElement)?.value; if(id&&t) api.execADB(id,`input text '${t}'`); }} className="px-2 py-1 bg-gray-700 rounded text-[10px]">Set</button>
              </div>
            </Panel>
            <Panel title="Sensors">
              <div className="grid grid-cols-2 gap-2">
                <div><div className="text-[9px] text-gray-500 mb-1">Accelerometer</div>
                  <input type="text" placeholder="x:y:z" defaultValue="0:0:9.8" id="sensor-accel" className="w-full bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] font-mono focus:outline-none focus:border-emerald-400" />
                  <button onClick={() => { const v=(document.getElementById('sensor-accel') as HTMLInputElement)?.value; if(id&&v) api.execADB(id,`emu sensor set acceleration ${v}`); }} className="mt-1 px-2 py-0.5 bg-gray-800 rounded text-[9px] hover:bg-gray-700">Apply</button>
                </div>
                <div><div className="text-[9px] text-gray-500 mb-1">Gyroscope</div>
                  <input type="text" placeholder="x:y:z" defaultValue="0:0:0" id="sensor-gyro" className="w-full bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] font-mono focus:outline-none focus:border-emerald-400" />
                  <button onClick={() => { const v=(document.getElementById('sensor-gyro') as HTMLInputElement)?.value; if(id&&v) api.execADB(id,`emu sensor set gyroscope ${v}`); }} className="mt-1 px-2 py-0.5 bg-gray-800 rounded text-[9px] hover:bg-gray-700">Apply</button>
                </div>
              </div>
            </Panel>
          </>}

          {/* ===== APPS TAB ===== */}
          {activeTab === 'apps' && <>
            <Panel title="Install APK">
              <div className="flex gap-1.5">
                <input type="text" placeholder="/path/to/app.apk" id="apk-path" className="flex-1 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] font-mono focus:outline-none focus:border-emerald-400" />
                <button onClick={() => { const p=(document.getElementById('apk-path') as HTMLInputElement)?.value; if(id&&p) api.execADB(id,`pm install -g ${p}`); }} className="px-2 py-1 bg-emerald-500/20 text-emerald-400 rounded text-[10px]">Install</button>
              </div>
            </Panel>
            <Panel title="App Management">
              <div className="space-y-2">
                <div className="flex gap-1.5">
                  <input type="text" placeholder="com.example.app" id="pkg-name" className="flex-1 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] font-mono focus:outline-none focus:border-emerald-400" />
                </div>
                <div className="flex gap-1 flex-wrap">
                  <button onClick={() => { const p=(document.getElementById('pkg-name') as HTMLInputElement)?.value; if(id&&p) api.execADB(id,`monkey -p ${p} -c android.intent.category.LAUNCHER 1`); }} className="px-2 py-1 bg-emerald-800/50 text-emerald-400 rounded text-[10px]">Launch</button>
                  <button onClick={() => { const p=(document.getElementById('pkg-name') as HTMLInputElement)?.value; if(id&&p) api.execADB(id,`am force-stop ${p}`); }} className="px-2 py-1 bg-red-800/50 text-red-400 rounded text-[10px]">Force Stop</button>
                  <button onClick={() => { const p=(document.getElementById('pkg-name') as HTMLInputElement)?.value; if(id&&p) api.execADB(id,`pm clear ${p}`); }} className="px-2 py-1 bg-orange-800/50 text-orange-400 rounded text-[10px]">Clear Data</button>
                  <button onClick={() => { const p=(document.getElementById('pkg-name') as HTMLInputElement)?.value; if(id&&p) api.execADB(id,`pm uninstall ${p}`); }} className="px-2 py-1 bg-red-800/50 text-red-400 rounded text-[10px]">Uninstall</button>
                </div>
              </div>
            </Panel>
            <Panel title="Permissions">
              <div className="space-y-1.5">
                <input type="text" placeholder="com.example.app" id="perm-pkg" className="w-full bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] font-mono focus:outline-none focus:border-emerald-400" />
                <div className="flex gap-1 flex-wrap">
                  {['CAMERA','RECORD_AUDIO','ACCESS_FINE_LOCATION','READ_CONTACTS','READ_EXTERNAL_STORAGE','WRITE_EXTERNAL_STORAGE'].map(perm => (
                    <button key={perm} onClick={() => { const p=(document.getElementById('perm-pkg') as HTMLInputElement)?.value; if(id&&p) api.execADB(id,`pm grant ${p} android.permission.${perm}`); }}
                      className="px-1.5 py-0.5 bg-gray-800 rounded text-[9px] font-mono hover:bg-gray-700">{perm}</button>
                  ))}
                </div>
              </div>
            </Panel>
            <Panel title="Deep Link">
              <div className="flex gap-1.5">
                <input type="text" placeholder="https://..." id="deeplink-url" className="flex-1 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] focus:outline-none focus:border-emerald-400"
                  onKeyDown={e => { if (e.key === 'Enter' && id) api.openDeeplink(id, (e.target as HTMLInputElement).value); }} />
                <button onClick={() => { const u=(document.getElementById('deeplink-url') as HTMLInputElement)?.value; if(id&&u) api.openDeeplink(id,u); }} className="px-2 py-1 bg-blue-500/20 text-blue-400 rounded text-[10px]">Open</button>
              </div>
            </Panel>
          </>}

          {/* ===== CAPTURE TAB ===== */}
          {activeTab === 'capture' && <>
            <div className="grid grid-cols-3 gap-3">
              <Panel title="Video">
                {!recording ? (
                  <button onClick={async () => { if(id){await api.startRecording(id);setRecording(true)} }} className="w-full px-2 py-1.5 bg-red-500/20 text-red-400 rounded text-[10px]">● Record</button>
                ) : (
                  <button onClick={async () => { if(id){await api.stopRecording(id);setRecording(false)} }} className="w-full px-2 py-1.5 bg-red-500 text-white rounded text-[10px] animate-pulse">■ Stop</button>
                )}
              </Panel>
              <Panel title="Network">
                {!harCapturing ? (
                  <button onClick={async () => { if(id){await api.startHAR(id);setHarCapturing(true)} }} className="w-full px-2 py-1.5 bg-blue-500/20 text-blue-400 rounded text-[10px]">Capture</button>
                ) : (
                  <button onClick={async () => { if(id){await api.stopHAR(id);setHarCapturing(false)} }} className="w-full px-2 py-1.5 bg-blue-500 text-white rounded text-[10px] animate-pulse">■ Stop</button>
                )}
              </Panel>
              <Panel title="Screenshot">
                <button onClick={async () => { if(!id) return; const blob = await api.takeScreenshot(id); const url = URL.createObjectURL(blob); const a = document.createElement('a'); a.href=url; a.download=`screenshot_${Date.now()}.png`; a.click(); URL.revokeObjectURL(url); }}
                  className="w-full px-2 py-1.5 bg-gray-700 text-gray-300 rounded text-[10px] hover:bg-gray-600">Capture</button>
              </Panel>
            </div>
            <Panel title="Snapshots">
              <div className="flex gap-1.5">
                <input type="text" placeholder="snapshot name" id="snap-name" className="flex-1 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] font-mono focus:outline-none focus:border-emerald-400" />
                <button onClick={() => { const n=(document.getElementById('snap-name') as HTMLInputElement)?.value; if(id&&n) api.execADB(id,`emu avd snapshot save ${n}`); }} className="px-2 py-1 bg-emerald-500/20 text-emerald-400 rounded text-[10px]">Save</button>
                <button onClick={() => { const n=(document.getElementById('snap-name') as HTMLInputElement)?.value; if(id&&n) api.execADB(id,`emu avd snapshot load ${n}`); }} className="px-2 py-1 bg-blue-500/20 text-blue-400 rounded text-[10px]">Restore</button>
              </div>
            </Panel>
            <LogcatPanel instanceId={id || ''} />
          </>}

          {/* ===== DEBUG TAB ===== */}
          {activeTab === 'debug' && <>
            <Panel title="ADB Shell">
              <div className="flex gap-1.5">
                <input type="text" placeholder="shell command..." value={adbCmd} onChange={e => setAdbCmd(e.target.value)}
                  className="flex-1 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] font-mono focus:outline-none focus:border-emerald-400"
                  onKeyDown={async e => { if (e.key==='Enter'&&id&&adbCmd) { const r=await api.execADB(id,adbCmd); setAdbOutput(r.output||r.error||''); setAdbCmd(''); } }} />
              </div>
              {adbOutput && <pre className="mt-1 text-[9px] text-gray-500 font-mono bg-gray-950 p-2 rounded max-h-32 overflow-auto">{adbOutput}</pre>}
            </Panel>
            <Panel title="Device Info">
              <button onClick={async () => { if(id) { const r = await fetch(`/api/v1/sessions/${id}/device-info`); setDeviceInfo(await r.json()); } }} className="px-2 py-1 bg-gray-800 rounded text-[10px] hover:bg-gray-700 mb-2">Refresh</button>
              {deviceInfo && <pre className="text-[9px] text-gray-500 font-mono bg-gray-950 p-2 rounded max-h-40 overflow-auto">{JSON.stringify(deviceInfo, null, 2)}</pre>}
            </Panel>
            <Panel title="Notifications">
              <button onClick={async () => { if(id) { const r = await api.execADB(id, 'dumpsys notification --noredact | grep -A2 NotificationRecord | head -20'); setAdbOutput(r.output||'none'); } }} className="px-2 py-1 bg-gray-800 rounded text-[10px] hover:bg-gray-700">Show</button>
            </Panel>
            <Panel title="File Transfer">
              <div className="space-y-2">
                <div className="flex gap-1.5">
                  <input type="text" placeholder="local path" id="push-local" className="flex-1 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] font-mono focus:outline-none focus:border-emerald-400" />
                  <input type="text" placeholder="device path" id="push-remote" className="flex-1 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] font-mono focus:outline-none focus:border-emerald-400" />
                  <button onClick={() => { const l=(document.getElementById('push-local') as HTMLInputElement)?.value; const r=(document.getElementById('push-remote') as HTMLInputElement)?.value; if(id&&l&&r) api.execADB(id,`push ${l} ${r}`); }} className="px-2 py-1 bg-gray-700 rounded text-[10px]">Push</button>
                </div>
                <div className="flex gap-1.5">
                  <input type="text" placeholder="device path" id="pull-remote" className="flex-1 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] font-mono focus:outline-none focus:border-emerald-400" />
                  <input type="text" placeholder="local path" id="pull-local" className="flex-1 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-[10px] font-mono focus:outline-none focus:border-emerald-400" />
                  <button onClick={() => { const r=(document.getElementById('pull-remote') as HTMLInputElement)?.value; const l=(document.getElementById('pull-local') as HTMLInputElement)?.value; if(id&&r&&l) api.execADB(id,`pull ${r} ${l}`); }} className="px-2 py-1 bg-gray-700 rounded text-[10px]">Pull</button>
                </div>
              </div>
            </Panel>
            <Panel title="UI Hierarchy">
              <button onClick={async () => { if(id) { const r = await api.execADB(id, 'uiautomator dump /dev/tty'); setAdbOutput(r.output||''); } }} className="px-2 py-1 bg-gray-800 rounded text-[10px] hover:bg-gray-700">Dump</button>
            </Panel>
          </>}
        </div>
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
    ws.onmessage = (e) => { if (paused) return; const nl = e.data.split('\n').filter((l: string) => l.trim()); setLines(prev => [...prev.slice(-200), ...nl]); if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight; };
    return () => ws.close();
  }, [instanceId, paused]);
  return (
    <div className="bg-gray-900 border border-gray-800 rounded-lg">
      <div className="flex items-center justify-between px-3 py-1.5 border-b border-gray-800">
        <span className="text-[10px] text-gray-500 uppercase tracking-wider">Logcat</span>
        <div className="flex gap-2">
          <button onClick={() => setPaused(!paused)} className={`text-[10px] px-2 py-0.5 rounded ${paused ? 'bg-yellow-500/20 text-yellow-400' : 'bg-gray-800 text-gray-400 hover:bg-gray-700'}`}>{paused ? 'Resume' : 'Pause'}</button>
          <button onClick={() => setLines([])} className="text-[10px] px-2 py-0.5 bg-gray-800 text-gray-400 rounded hover:bg-gray-700">Clear</button>
        </div>
      </div>
      <pre ref={logRef} className="p-2 text-[8px] font-mono text-gray-500 h-[150px] overflow-auto leading-tight whitespace-pre-wrap">{lines.length === 0 ? 'Waiting for logs...' : lines.join('\n')}</pre>
    </div>
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

function NavBtn({ onClick, children }: { onClick: () => void; children: React.ReactNode }) {
  return <button onClick={onClick} className="px-2 py-1 bg-gray-800 rounded text-[10px] hover:bg-gray-700">{children}</button>;
}
