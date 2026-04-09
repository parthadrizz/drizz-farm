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

async function fetchText(url: string): Promise<string> {
  const resp = await fetch(BASE + url);
  if (!resp.ok) throw new Error(`API error ${resp.status}`);
  return resp.text();
}

export const api = {
  // Pool
  pool: () => fetchJSON<PoolStatus>('/pool'),
  available: (profile?: string) => fetchJSON<{ available: number }>(`/pool/available${profile ? `?profile=${profile}` : ''}`),
  bootAVD: (avdName: string) => fetchJSON<any>('/pool/boot', { method: 'POST', body: JSON.stringify({ avd_name: avdName }) }),
  shutdownInstance: (instanceId: string) => fetchJSON<any>('/pool/shutdown', { method: 'POST', body: JSON.stringify({ instance_id: instanceId }) }),

  // Sessions
  createSession: (profile: string) =>
    fetchJSON<Session>('/sessions', { method: 'POST', body: JSON.stringify({ profile, source: 'dashboard' }) }),
  listSessions: () => fetchJSON<{ sessions: Session[]; active: number; queued: number }>('/sessions'),
  getSession: (id: string) => fetchJSON<Session>(`/sessions/${id}`),
  releaseSession: (id: string) => fetchJSON<{ status: string }>(`/sessions/${id}`, { method: 'DELETE' }),

  // Node
  health: () => fetchJSON<NodeHealth>('/node/health'),

  // Config
  getConfig: () => fetchJSON<any>('/config'),
  getConfigRaw: () => fetchText('/config/raw'),
  saveConfigRaw: (yaml: string) =>
    fetch(BASE + '/config/raw', { method: 'PUT', body: yaml, headers: { 'Content-Type': 'text/yaml' } }).then(r => r.json()),

  // Discovery
  systemImages: () => fetchJSON<{ images: SystemImage[] }>('/discovery/system-images'),
  devices: () => fetchJSON<{ devices: string[] }>('/discovery/devices'),
  avds: () => fetchJSON<{ avds: { name: string }[] }>('/discovery/avds'),
  createAVDs: (data: { profile_name: string; device: string; system_image: string; count: number }) =>
    fetchJSON<{ created: number; errors: string[] }>('/discovery/create-avds', { method: 'POST', body: JSON.stringify(data) }),
};
