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
    <div>
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
    <div onClick={onClick} className="cursor-pointer group">
      <div className="bg-gray-900 border border-gray-800 rounded-lg overflow-hidden hover:border-emerald-400/50 transition w-[168px] h-[373px] relative">
        {!connected ? (
          <div className="w-full h-full flex items-center justify-center bg-gray-950">
            <div className="animate-spin w-5 h-5 border-2 border-gray-700 border-t-emerald-400 rounded-full" />
          </div>
        ) : (
          <canvas ref={canvasRef} className="w-full h-full object-cover" />
        )}
        <div className="absolute inset-0 bg-black/0 group-hover:bg-black/20 transition" />
      </div>
      <div className="mt-1.5 flex items-center gap-1.5 px-0.5">
        <div className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${
          instance.state === 'allocated' ? 'bg-blue-400' : 'bg-emerald-400'
        }`} />
        <span className="text-[11px] text-gray-400 truncate">{instance.device_name}</span>
      </div>
    </div>
  );
}
