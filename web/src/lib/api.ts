const BASE = '/api/v1';

export interface PoolStatus {
  total_capacity: number;
  warm: number;
  allocated: number;
  booting: number;
  resetting: number;
  error: number;
  instances: DeviceInstance[];
}

export interface DeviceInstance {
  id: string;
  device_kind: string;
  device_name: string;
  profile: string;
  state: string;
  serial: string;
  connection: ConnectionInfo;
  session_id?: string;
  created_at: string;
  last_activity: string;
  health_fails: number;
}

export interface ConnectionInfo {
  host: string;
  device_kind?: string;
  adb_port?: number;
  adb_serial?: string;
  console_port?: number;
}

export interface Session {
  id: string;
  profile: string;
  platform: string;
  instance_id: string;
  state: string;
  connection: ConnectionInfo;
  created_at: string;
  expires_at: string;
}

export interface NodeHealth {
  status: string;
  node: string;
  version: string;
  uptime: string;
  pool: { capacity: number; warm: number; allocated: number; booting: number; error: number };
  sessions: { active: number; queued: number };
  resources: { goroutines: number; num_cpu: number; heap_alloc: number };
}

export interface SystemImage {
  path: string;
  api_name: string;
  variant: string;
  arch: string;
}

async function fetchJSON<T>(url: string, opts?: RequestInit): Promise<T> {
  const resp = await fetch(BASE + url, {
    headers: { 'Content-Type': 'application/json' },
    ...opts,
  });
  if (!resp.ok) {
    const body = await resp.text();
    throw new Error(`API error ${resp.status}: ${body}`);
  }
  return resp.json();
}

export const api = {
  pool: () => fetchJSON<PoolStatus>('/pool'),
  health: () => fetchJSON<NodeHealth>('/node/health'),
  createSession: (profile: string) =>
    fetchJSON<Session>('/sessions', { method: 'POST', body: JSON.stringify({ profile, source: 'dashboard' }) }),
  listSessions: () => fetchJSON<{ sessions: Session[]; active: number; queued: number }>('/sessions'),
  releaseSession: (id: string) => fetchJSON<{ status: string }>(`/sessions/${id}`, { method: 'DELETE' }),
  systemImages: () => fetchJSON<{ images: SystemImage[] }>('/discovery/system-images'),
  devices: () => fetchJSON<{ devices: string[] }>('/discovery/devices'),
  avds: () => fetchJSON<{ avds: { name: string }[] }>('/discovery/avds'),
  createAVDs: (data: { profile_name: string; device: string; system_image: string; count: number }) =>
    fetchJSON<{ created: number; errors: string[] }>('/discovery/create-avds', { method: 'POST', body: JSON.stringify(data) }),
};
