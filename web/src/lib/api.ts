const BASE = '/api/v1';

export interface PoolStatus {
  node_name: string;
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
  node_name: string;
  device_kind: string;
  display_info: string;
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
  node_name?: string;
  device_kind?: string;
  adb_port?: number;
  adb_serial?: string;
  console_port?: number;
}

export interface Session {
  id: string;
  node_name: string;
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
  group: { name: string };
  version: string;
  uptime: string;
  pool: { capacity: number; warm: number; allocated: number; booting: number; error: number };
  sessions: { active: number; queued: number };
  resources: { goroutines: number; num_cpu: number; heap_alloc: number };
}

export interface NodeEntry {
  name: string;
  url: string;
  added_at?: string;
  description?: string;
}

export interface GroupInfo {
  group_name: string;
  group_key?: string; // present when this node is in a group; dashboard masks it behind an eye toggle
  has_group: boolean;
  self: { name: string; url: string };
}

export interface NodeList {
  group_name: string;
  nodes: NodeEntry[];
}

export interface CreateGroupResult {
  group_name: string;
  group_key: string;
  self: NodeEntry;
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

  // Device simulation
  setGPS: (id: string, lat: number, lng: number) => fetchJSON<any>(`/sessions/${id}/gps`, { method: 'POST', body: JSON.stringify({ latitude: lat, longitude: lng }) }),
  setNetwork: (id: string, profile: string) => fetchJSON<any>(`/sessions/${id}/network`, { method: 'POST', body: JSON.stringify({ profile }) }),
  setBattery: (id: string, level: number, charging: string) => fetchJSON<any>(`/sessions/${id}/battery`, { method: 'POST', body: JSON.stringify({ level, charging }) }),
  setOrientation: (id: string, rotation: number) => fetchJSON<any>(`/sessions/${id}/orientation`, { method: 'POST', body: JSON.stringify({ rotation }) }),
  setDarkMode: (id: string, dark: boolean) => fetchJSON<any>(`/sessions/${id}/appearance`, { method: 'POST', body: JSON.stringify({ dark }) }),
  setLocale: (id: string, locale: string) => fetchJSON<any>(`/sessions/${id}/locale`, { method: 'POST', body: JSON.stringify({ locale }) }),
  openDeeplink: (id: string, url: string) => fetchJSON<any>(`/sessions/${id}/deeplink`, { method: 'POST', body: JSON.stringify({ url }) }),
  execADB: (id: string, command: string) => fetchJSON<any>(`/sessions/${id}/adb`, { method: 'POST', body: JSON.stringify({ command }) }),

  // Recording + Artifacts
  startRecording: (id: string) => fetchJSON<any>(`/sessions/${id}/recording/start`, { method: 'POST' }),
  stopRecording: (id: string) => fetchJSON<any>(`/sessions/${id}/recording/stop`, { method: 'POST' }),
  listRecordings: (id: string) => fetchJSON<any>(`/sessions/${id}/recordings`),
  takeScreenshot: (id: string) => fetch(`${BASE}/sessions/${id}/screenshot`, { method: 'POST' }).then(r => r.blob()),
  startHAR: (id: string) => fetchJSON<any>(`/sessions/${id}/har/start`, { method: 'POST' }),
  stopHAR: (id: string) => fetchJSON<any>(`/sessions/${id}/har/stop`, { method: 'POST' }),

  // Group / registry (this node's view of the group)
  groupInfo: () => fetchJSON<GroupInfo>('/group'),
  listNodes: () => fetchJSON<NodeList>('/nodes'),
  createGroup: (name: string) => fetchJSON<CreateGroupResult>('/group', { method: 'POST', body: JSON.stringify({ name }) }),
  joinGroup: (peerURL: string, groupKey: string) =>
    fetchJSON<{ status: string; group_name: string }>('/group/join', {
      method: 'POST', body: JSON.stringify({ peer_url: peerURL, group_key: groupKey }),
    }),
  leaveGroup: () => fetchJSON<{ status: string }>('/group', { method: 'DELETE' }),
  addNode: (key: string, node: NodeEntry) =>
    fetchJSON<any>('/nodes', { method: 'POST', body: JSON.stringify(node), headers: { 'X-Group-Key': key, 'Content-Type': 'application/json' } }),
  removeNode: (key: string, name: string) =>
    fetchJSON<any>(`/nodes/${encodeURIComponent(name)}`, { method: 'DELETE', headers: { 'X-Group-Key': key } }),

  // Cross-node calls — talk directly to the peer's URL from the browser.
  // `nodeURL` is the peer's external_url from /nodes (e.g. http://mac-2.local:9401).
  peer: {
    pool: (nodeURL: string) => fetch(`${nodeURL}/api/v1/pool`).then(r => r.json()),
    health: (nodeURL: string) => fetch(`${nodeURL}/api/v1/node/health`).then(r => r.json()),
    avds: (nodeURL: string) => fetch(`${nodeURL}/api/v1/discovery/avds`).then(r => r.json()),
    boot: (nodeURL: string, avdName: string) =>
      fetch(`${nodeURL}/api/v1/pool/boot`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ avd_name: avdName }) }).then(r => r.json()),
    shutdown: (nodeURL: string, instanceId: string) =>
      fetch(`${nodeURL}/api/v1/pool/shutdown`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ instance_id: instanceId }) }).then(r => r.json()),
  },

  // Discovery
  systemImages: () => fetchJSON<{ images: SystemImage[] }>('/discovery/system-images'),
  availableImages: () => fetchJSON<{ images: SystemImage[] }>('/discovery/available-images'),
  installImage: (path: string) => fetchJSON<{ status: string }>('/discovery/install-image', { method: 'POST', body: JSON.stringify({ path }) }),
  // Streaming install — returns a fetch Response so the caller can read
  // the body as a ReadableStream and surface real-time sdkmanager
  // output. Last line is __STATUS__:ok or __STATUS__:error: <msg>.
  installImageStream: (path: string) =>
    fetch('/api/v1/discovery/install-image-stream', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path }),
    }),
  devices: () => fetchJSON<{ devices: string[] }>('/discovery/devices'),
  avds: () => fetchJSON<{ avds: { name: string }[] }>('/discovery/avds'),
  createAVDs: (data: {
    profile_name: string;
    device: string;
    system_image: string;
    count: number;
    ram_mb?: number;
    heap_mb?: number;
    disk_size_mb?: number;
    gpu?: string;
  }) =>
    fetchJSON<{ created: number; errors: string[] }>('/discovery/create-avds', { method: 'POST', body: JSON.stringify(data) }),
};
