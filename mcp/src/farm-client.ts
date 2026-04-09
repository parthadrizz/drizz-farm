// HTTP client for drizz-farm REST API

export class FarmClient {
  private baseUrl: string;

  constructor(host: string = '127.0.0.1', port: number = 9401) {
    this.baseUrl = `http://${host}:${port}/api/v1`;
  }

  async isAvailable(): Promise<boolean> {
    try {
      const r = await this.get('/node/health');
      return r.status === 'healthy';
    } catch { return false; }
  }

  // Sessions
  async createSession(profile?: string) {
    return this.post('/sessions', { profile: profile || '', source: 'mcp' });
  }
  async releaseSession(id: string) {
    return this.del(`/sessions/${id}`);
  }
  async listSessions() {
    return this.get('/sessions');
  }

  // Pool
  async poolStatus() { return this.get('/pool'); }

  // Device interaction
  async tap(id: string, target: string) {
    // If target looks like coordinates "x,y", use input tap
    if (/^\d+,\d+$/.test(target)) {
      const [x, y] = target.split(',');
      return this.post(`/sessions/${id}/adb`, { command: `input tap ${x} ${y}` });
    }
    // Otherwise find element by text in UI tree and tap its center
    const tree = await this.get(`/sessions/${id}/ui-tree`);
    const bounds = this.findBoundsForText(tree.tree, target);
    if (bounds) {
      return this.post(`/sessions/${id}/adb`, { command: `input tap ${bounds.x} ${bounds.y}` });
    }
    return { error: `element "${target}" not found` };
  }

  async typeText(id: string, text: string) {
    // Escape special chars for adb input
    const escaped = text.replace(/ /g, '%s').replace(/'/g, "\\'");
    return this.post(`/sessions/${id}/adb`, { command: `input text '${escaped}'` });
  }

  async swipe(id: string, direction: string, distance: string = 'medium') {
    const dists: Record<string, number> = { short: 300, medium: 600, long: 1000 };
    const d = dists[distance] || 600;
    const cmds: Record<string, string> = {
      up: `input swipe 540 ${800 + d} 540 800 300`,
      down: `input swipe 540 800 540 ${800 + d} 300`,
      left: `input swipe ${540 + d} 1200 540 1200 300`,
      right: `input swipe 540 1200 ${540 + d} 1200 300`,
    };
    return this.post(`/sessions/${id}/adb`, { command: cmds[direction] || cmds.up });
  }

  async screenshot(id: string) { return this.post(`/sessions/${id}/screenshot`, {}); }
  async getUITree(id: string) { return this.get(`/sessions/${id}/ui-tree`); }
  async getScreenText(id: string) { return this.get(`/sessions/${id}/screen-text`); }
  async getActivity(id: string) { return this.get(`/sessions/${id}/activity`); }
  async getDeviceInfo(id: string) { return this.get(`/sessions/${id}/device-info`); }
  async getLogs(id: string, lines: number = 100) { return this.get(`/sessions/${id}/logcat/download?lines=${lines}`); }
  async isKeyboardShown(id: string) { return this.get(`/sessions/${id}/keyboard`); }
  async getNotifications(id: string) { return this.get(`/sessions/${id}/notifications`); }

  // Device simulation
  async setGPS(id: string, lat: number, lng: number) { return this.post(`/sessions/${id}/gps`, { latitude: lat, longitude: lng }); }
  async setNetwork(id: string, profile: string) { return this.post(`/sessions/${id}/network`, { profile }); }
  async setBattery(id: string, level: number) { return this.post(`/sessions/${id}/battery`, { level, charging: level > 20 ? 'ac' : 'none' }); }
  async setOrientation(id: string, rotation: number) { return this.post(`/sessions/${id}/orientation`, { rotation }); }
  async setDarkMode(id: string, dark: boolean) { return this.post(`/sessions/${id}/appearance`, { dark }); }
  async setLocale(id: string, locale: string) { return this.post(`/sessions/${id}/locale`, { locale }); }

  // App management
  async installApp(id: string, path: string) { return this.post(`/sessions/${id}/install`, { path }); }
  async uninstallApp(id: string, pkg: string) { return this.post(`/sessions/${id}/uninstall`, { package: pkg }); }
  async clearData(id: string, pkg: string) { return this.post(`/sessions/${id}/clear-data`, { package: pkg }); }
  async launchApp(id: string, pkg: string) { return this.post(`/sessions/${id}/launch`, { package: pkg }); }
  async forceStop(id: string, pkg: string) { return this.post(`/sessions/${id}/force-stop`, { package: pkg }); }

  // Deep link
  async openDeeplink(id: string, url: string) { return this.post(`/sessions/${id}/deeplink`, { url }); }

  // Files
  async pushFile(id: string, local: string, remote: string) { return this.post(`/sessions/${id}/file/push`, { local_path: local, device_path: remote }); }
  async pullFile(id: string, remote: string, local: string) { return this.post(`/sessions/${id}/file/pull`, { device_path: remote, local_path: local }); }

  // Smart helpers
  async waitForElement(id: string, text: string, timeout: number = 10) { return this.post(`/sessions/${id}/wait-for`, { text, timeout_seconds: timeout }); }
  async scrollToText(id: string, text: string) { return this.post(`/sessions/${id}/scroll-to`, { text }); }
  async pressKey(id: string, keycode: string) { return this.post(`/sessions/${id}/key`, { keycode }); }
  async longPress(id: string, x: number, y: number) { return this.post(`/sessions/${id}/long-press`, { x, y }); }

  // Raw ADB
  async execADB(id: string, command: string) { return this.post(`/sessions/${id}/adb`, { command }); }

  // Recording
  async startRecording(id: string) { return this.post(`/sessions/${id}/recording/start`, {}); }
  async stopRecording(id: string) { return this.post(`/sessions/${id}/recording/stop`, {}); }
  async startHAR(id: string) { return this.post(`/sessions/${id}/har/start`, {}); }
  async stopHAR(id: string) { return this.post(`/sessions/${id}/har/stop`, {}); }

  // Crash detection
  async checkCrash(id: string, pkg: string) {
    const result = await this.execADB(id, `logcat -d -b crash | grep ${pkg} | tail -5`);
    return { crashed: (result.output || '').trim().length > 0, output: result.output };
  }

  // --- HTTP helpers ---
  private async get(path: string): Promise<any> {
    const resp = await fetch(this.baseUrl + path);
    if (!resp.ok) throw new Error(`API ${resp.status}: ${await resp.text()}`);
    const ct = resp.headers.get('content-type') || '';
    if (ct.includes('json')) return resp.json();
    return { text: await resp.text() };
  }

  private async post(path: string, body: any): Promise<any> {
    const resp = await fetch(this.baseUrl + path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (!resp.ok) throw new Error(`API ${resp.status}: ${await resp.text()}`);
    const ct = resp.headers.get('content-type') || '';
    if (ct.includes('json')) return resp.json();
    return { text: await resp.text() };
  }

  private async del(path: string): Promise<any> {
    const resp = await fetch(this.baseUrl + path, { method: 'DELETE' });
    if (!resp.ok) throw new Error(`API ${resp.status}: ${await resp.text()}`);
    return resp.json();
  }

  private findBoundsForText(xml: string, text: string): { x: number; y: number } | null {
    // Parse bounds from UI XML: bounds="[left,top][right,bottom]"
    const regex = new RegExp(`text="${text}"[^>]*bounds="\\[(\\d+),(\\d+)\\]\\[(\\d+),(\\d+)\\]"`);
    const match = xml?.match(regex);
    if (match) {
      const [, l, t, r, b] = match.map(Number);
      return { x: Math.round((l + r) / 2), y: Math.round((t + b) / 2) };
    }
    return null;
  }
}
