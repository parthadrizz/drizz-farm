import { useEffect, useRef, useState } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { Tv } from 'lucide-react';
import { api, DeviceInstance } from '../lib/api';
import { EmptyState } from '../components/EmptyState';

// Flattened list of live devices across all nodes, each tagged with its node URL
// so the screen stream connects directly to the right daemon.
interface LiveDevice {
  inst: DeviceInstance;
  nodeName: string;
  nodeURL: string;
}

export function GridView() {
  const [live, setLive] = useState<LiveDevice[] | null>(null);
  const navigate = useNavigate();

  useEffect(() => {
    const refresh = async () => {
      try {
        const list = await api.listNodes();
        const results = await Promise.all(
          list.nodes.map(async (node) => {
            try {
              const pool = await api.peer.pool(node.url);
              return (pool.instances || [])
                .filter((i: DeviceInstance) => i.state === 'warm' || i.state === 'allocated')
                .map((inst: DeviceInstance) => ({ inst, nodeName: node.name, nodeURL: node.url }));
            } catch {
              return [] as LiveDevice[];
            }
          })
        );
        setLive(results.flat());
      } catch {
        setLive([]);
      }
    };
    refresh();
    const i = setInterval(refresh, 5000);
    return () => clearInterval(i);
  }, []);

  if (live === null) return (
    <div className="flex items-center justify-center py-20">
      <div className="w-5 h-5 border-2 border-muted border-t-primary rounded-full animate-spin" />
      <span className="ml-3 text-muted-foreground text-sm">Loading...</span>
    </div>
  );

  if (live.length === 0) return (
    <div className="section-card">
      <EmptyState
        icon={Tv}
        title="No live devices"
        description={
          <>
            Boot an emulator from the <Link to="/" className="text-primary hover:underline">Dashboard</Link> to see its
            screen mirrored here in real time. Click any tile to open a full-screen
            interactive view with input + recording.
          </>
        }
        primary={{ label: 'Go to Dashboard', to: '/' }}
      />
    </div>
  );

  const sorted = [...live].sort((a, b) => a.inst.device_name.localeCompare(b.inst.device_name));

  return (
    <div className="animate-fade-in">
      <div className="flex flex-wrap gap-5 justify-center">
        {sorted.map(d => (
          <StreamTile
            key={`${d.nodeURL}/${d.inst.id}`}
            device={d}
            onClick={() => navigate(`/live/${d.inst.id}?node=${encodeURIComponent(d.nodeURL)}`)}
          />
        ))}
      </div>
    </div>
  );
}

function StreamTile({ device, onClick }: { device: LiveDevice; onClick: () => void }) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    // Always connect directly to the node that owns this instance.
    // Turn http://mac-1.local:9401 into ws://mac-1.local:9401 (or wss:// for https).
    const nodeURL = new URL(device.nodeURL);
    const proto = nodeURL.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsURL = `${proto}//${nodeURL.host}/api/v1/sessions/${device.inst.id}/screen`;
    const ws = new WebSocket(wsURL);
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
  }, [device.nodeURL, device.inst.id]);

  return (
    <div className="section-card overflow-hidden cursor-pointer" style={{ width: 320 }} onClick={onClick}>
      <canvas ref={canvasRef} className="block" style={{ width: 320, height: 711, background: '#000' }} />
      <div className="px-4 py-3">
        <div className="text-[13px] font-mono text-foreground">{device.inst.device_name}</div>
        <div className="text-[11px] text-purple-400 mt-0.5">{device.inst.display_info}</div>
        <div className="text-[10px] text-muted-foreground/60 mt-0.5">
          {device.nodeName} · {connected ? 'streaming' : 'connecting…'}
        </div>
      </div>
    </div>
  );
}
