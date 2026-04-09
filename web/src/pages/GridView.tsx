import { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api, PoolStatus, DeviceInstance } from '../lib/api';

export function GridView() {
  const [pool, setPool] = useState<PoolStatus | null>(null);
  const navigate = useNavigate();

  useEffect(() => {
    const refresh = async () => {
      try { setPool(await api.pool()); } catch {}
    };
    refresh();
    const i = setInterval(refresh, 5000);
    return () => clearInterval(i);
  }, []);

  if (!pool) return <div className="text-center py-20 text-gray-500">Loading...</div>;

  const liveInstances = pool.instances.filter(i => i.state === 'warm' || i.state === 'allocated');

  if (liveInstances.length === 0) {
    return (
      <div className="text-center py-20">
        <div className="text-gray-400 text-lg mb-2">No live devices</div>
        <div className="text-gray-600 text-sm">Start an emulator from the Dashboard to see it here.</div>
      </div>
    );
  }

  // 3-4 columns so tiles stay compact
  const cols = liveInstances.length <= 3 ? 3 : liveInstances.length <= 8 ? 4 : 5;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Live Grid</h1>
        <span className="text-sm text-gray-500">{liveInstances.length} device{liveInstances.length > 1 ? 's' : ''}</span>
      </div>

      <div className={`grid gap-3`} style={{ gridTemplateColumns: `repeat(${cols}, 1fr)` }}>
        {liveInstances.map(inst => (
          <StreamTile key={inst.id} instance={inst} onClick={() => navigate(`/live/${inst.id}`)} />
        ))}
      </div>
    </div>
  );
}

function StreamTile({ instance, onClick }: { instance: DeviceInstance; onClick: () => void }) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${proto}//${window.location.host}/api/v1/sessions/${instance.id}/screen`);
    ws.binaryType = 'arraybuffer';

    ws.onopen = () => setConnected(true);
    ws.onclose = () => setConnected(false);

    ws.onmessage = (event) => {
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
  }, [instance.id]);

  return (
    <div onClick={onClick}
      className="bg-gray-900 border border-gray-800 rounded-lg overflow-hidden cursor-pointer hover:border-emerald-400/50 transition group max-w-[200px]">
      <div className="relative">
        {!connected ? (
          <div className="aspect-[9/16] flex items-center justify-center bg-gray-950">
            <div className="animate-spin w-5 h-5 border-2 border-gray-700 border-t-emerald-400 rounded-full" />
          </div>
        ) : (
          <canvas ref={canvasRef} className="w-full aspect-[9/16] object-contain" />
        )}
        {/* Overlay on hover */}
        <div className="absolute inset-0 bg-black/0 group-hover:bg-black/30 transition flex items-center justify-center">
          <span className="opacity-0 group-hover:opacity-100 text-white text-sm font-medium transition">Open</span>
        </div>
      </div>
      <div className="px-3 py-2 flex items-center justify-between">
        <div className="min-w-0">
          <div className="text-xs font-medium text-gray-300 truncate">{instance.device_name}</div>
          <div className="text-[10px] text-gray-600">{instance.serial}</div>
        </div>
        <div className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${
          instance.state === 'allocated' ? 'bg-blue-400' : 'bg-emerald-400'
        }`} />
      </div>
    </div>
  );
}
