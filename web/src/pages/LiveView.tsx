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

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <button onClick={() => navigate('/')} className="text-sm text-gray-500 hover:text-gray-300">← Back</button>
          <h1 className="text-xl font-bold">
            {instance?.device_name || id}
          </h1>
          <div className={`w-2 h-2 rounded-full ${connected ? 'bg-emerald-400' : 'bg-red-400'}`} />
          <span className="text-xs text-gray-500">{connected ? `${fps} fps` : 'disconnected'}</span>
        </div>

        {/* Quick actions */}
        <div className="flex gap-2">
          <button onClick={() => sendInput('back')} className="px-3 py-1.5 bg-gray-800 rounded text-xs hover:bg-gray-700 transition">Back</button>
          <button onClick={() => sendInput('home')} className="px-3 py-1.5 bg-gray-800 rounded text-xs hover:bg-gray-700 transition">Home</button>
          <button onClick={() => sendInput('recent')} className="px-3 py-1.5 bg-gray-800 rounded text-xs hover:bg-gray-700 transition">Recent</button>
        </div>
      </div>

      {/* Screen */}
      <div className="flex justify-center">
        <div className="bg-gray-900 border border-gray-800 rounded-lg p-2 inline-block">
          {!connected ? (
            <div className="w-[360px] h-[640px] flex items-center justify-center text-gray-500">
              <div className="text-center">
                <div className="animate-spin w-6 h-6 border-2 border-gray-600 border-t-emerald-400 rounded-full mx-auto mb-3" />
                <div className="text-sm">Connecting to screen...</div>
                <div className="text-xs text-gray-600 mt-1">{id}</div>
              </div>
            </div>
          ) : (
            <canvas
              ref={canvasRef}
              onClick={handleCanvasClick}
              className="max-w-[400px] max-h-[800px] cursor-crosshair rounded"
              style={{ imageRendering: 'auto' }}
            />
          )}
        </div>
      </div>

      {/* Info bar */}
      {instance && (
        <div className="flex justify-center">
          <div className="text-xs text-gray-500 space-x-4">
            <span>Serial: {instance.serial}</span>
            <span>ADB: {instance.connection.host}:{instance.connection.adb_port}</span>
            <span>State: {instance.state}</span>
          </div>
        </div>
      )}
    </div>
  );
}
