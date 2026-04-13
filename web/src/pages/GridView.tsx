import { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api, PoolStatus, DeviceInstance } from '../lib/api';

export function GridView() {
  const [pool, setPool] = useState<PoolStatus | null>(null);
  const navigate = useNavigate();

  useEffect(() => {
    const refresh = async () => {
      try {
        const localPool = await api.pool();
        setPool(localPool);
        const fed = await api.federationStatus().catch(() => null);
        if (fed?.nodes) {
          const peerNodes = fed.nodes.filter((n: any) => n.role !== 'self' && n.healthy);
          const peerInstances: DeviceInstance[] = [];
          await Promise.all(peerNodes.map(async (n: any) => {
            try { const rp = await api.remotePool(n.host); peerInstances.push(...(rp.instances || [])); } catch {}
          }));
          if (peerInstances.length > 0) {
            setPool(prev => prev ? { ...prev, instances: [...prev.instances, ...peerInstances] } : prev);
          }
        }
      } catch {}
    };
    refresh();
    const i = setInterval(refresh, 5000);
    return () => clearInterval(i);
  }, []);

  if (!pool) return (
    <div className="flex items-center justify-center py-20">
      <div className="w-5 h-5 border-2 border-muted border-t-primary rounded-full animate-spin" />
      <span className="ml-3 text-muted-foreground text-sm">Loading...</span>
    </div>
  );

  const liveInstances = pool.instances.filter(i => i.state === 'warm' || i.state === 'allocated');

  if (liveInstances.length === 0) {
    return (
      <div className="text-center py-24 animate-fade-in">
        <div className="text-foreground text-base mb-2">No live devices</div>
        <div className="text-muted-foreground text-sm">Start an emulator from the Dashboard to see it here.</div>
      </div>
    );
  }

  const sorted = [...liveInstances].sort((a, b) => a.device_name.localeCompare(b.device_name));

  return (
    <div className="animate-fade-in">
      <div className="flex flex-wrap gap-5 justify-center">
        {sorted.map(inst => (
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
        if (canvas.width !== 320 || canvas.height !== 711) { canvas.width = 320; canvas.height = 711; }
        canvas.getContext('2d')?.drawImage(img, 0, 0, 320, 711);
        URL.revokeObjectURL(url);
      };
      img.src = url;
    };
    return () => ws.close();
  }, [instance.id]);

  return (
    <div onClick={onClick} className="cursor-pointer group">
      <div className="section-card overflow-hidden hover:border-primary/50 transition-all duration-200 w-[320px] h-[711px] relative">
        {!connected ? (
          <div className="w-full h-full flex items-center justify-center surface-0">
            <div className="w-5 h-5 border-2 border-muted border-t-primary rounded-full animate-spin" />
          </div>
        ) : (
          <canvas ref={canvasRef} className="w-full h-full object-cover" />
        )}
        <div className="absolute inset-0 bg-transparent group-hover:bg-black/10 transition" />
      </div>
      <div className="mt-2.5 text-center">
        <div className="text-[11px] text-purple-400 font-mono">{instance.device_name}</div>
        {instance.display_info && <div className="text-[9px] text-muted-foreground">{instance.display_info}</div>}
        {instance.node_name && <div className="text-[9px] text-muted-foreground/50 font-mono">{instance.node_name}</div>}
      </div>
    </div>
  );
}
