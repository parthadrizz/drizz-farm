#!/usr/bin/env node

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { FarmClient } from "./farm-client.js";

const farm = new FarmClient(
  process.env.DRIZZ_FARM_HOST || '127.0.0.1',
  parseInt(process.env.DRIZZ_FARM_PORT || '9401')
);

const server = new McpServer({
  name: "prodigy-core",
  version: "0.1.0",
});

// --- Tool 1: device_session ---
server.tool(
  "device_session",
  "Manage emulator sessions. Create a session to get a device, release when done, or list available.",
  {
    action: z.enum(["create", "release", "list", "status"]).describe("Action to perform"),
    session_id: z.string().optional().describe("Session ID (for release)"),
    profile: z.string().optional().describe("Device profile (for create, optional)"),
  },
  async ({ action, session_id, profile }) => {
    if (!(await farm.isAvailable())) {
      return { content: [{ type: "text", text: "drizz-farm not found. Install: curl -fsSL https://get.drizz.dev/farm | bash" }] };
    }

    switch (action) {
      case "create": {
        const sess = await farm.createSession(profile);
        return { content: [{ type: "text", text: JSON.stringify(sess, null, 2) }] };
      }
      case "release": {
        if (!session_id) return { content: [{ type: "text", text: "session_id required" }] };
        const r = await farm.releaseSession(session_id);
        return { content: [{ type: "text", text: JSON.stringify(r) }] };
      }
      case "list": {
        const r = await farm.listSessions();
        return { content: [{ type: "text", text: JSON.stringify(r, null, 2) }] };
      }
      case "status": {
        const r = await farm.poolStatus();
        return { content: [{ type: "text", text: JSON.stringify(r, null, 2) }] };
      }
    }
  }
);

// --- Tool 2: device_interact ---
server.tool(
  "device_interact",
  "Interact with the emulator screen. Tap elements, type text, swipe, press keys, long press, or scroll to find text.",
  {
    session_id: z.string().describe("Session or instance ID"),
    action: z.enum(["tap", "type", "swipe", "press_key", "long_press", "scroll_to", "wait_for"]).describe("Interaction type"),
    target: z.string().optional().describe("Element text, resource-id, or 'x,y' coordinates (for tap)"),
    text: z.string().optional().describe("Text to type (for type action)"),
    direction: z.enum(["up", "down", "left", "right"]).optional().describe("Swipe direction"),
    keycode: z.string().optional().describe("Android keycode: ENTER, BACK, HOME, TAB, DELETE, etc."),
    x: z.number().optional().describe("X coordinate"),
    y: z.number().optional().describe("Y coordinate"),
    timeout: z.number().optional().describe("Timeout in seconds (for wait_for)"),
  },
  async ({ session_id, action, target, text, direction, keycode, x, y, timeout }) => {
    switch (action) {
      case "tap":
        return result(await farm.tap(session_id, target || `${x},${y}`));
      case "type":
        return result(await farm.typeText(session_id, text || ''));
      case "swipe":
        return result(await farm.swipe(session_id, direction || 'up'));
      case "press_key":
        return result(await farm.pressKey(session_id, keycode || 'ENTER'));
      case "long_press":
        return result(await farm.longPress(session_id, x || 0, y || 0));
      case "scroll_to":
        return result(await farm.scrollToText(session_id, target || text || ''));
      case "wait_for":
        return result(await farm.waitForElement(session_id, target || text || '', timeout || 10));
    }
  }
);

// --- Tool 3: device_observe ---
server.tool(
  "device_observe",
  "Observe the emulator screen. Take screenshots, get the UI element tree, read screen text, check which activity is showing, or check keyboard state.",
  {
    session_id: z.string().describe("Session or instance ID"),
    action: z.enum(["screenshot", "ui_tree", "screen_text", "activity", "keyboard", "notifications", "device_info"]).describe("What to observe"),
  },
  async ({ session_id, action }) => {
    switch (action) {
      case "screenshot":
        return result(await farm.screenshot(session_id));
      case "ui_tree":
        return result(await farm.getUITree(session_id));
      case "screen_text":
        return result(await farm.getScreenText(session_id));
      case "activity":
        return result(await farm.getActivity(session_id));
      case "keyboard":
        return result(await farm.isKeyboardShown(session_id));
      case "notifications":
        return result(await farm.getNotifications(session_id));
      case "device_info":
        return result(await farm.getDeviceInfo(session_id));
    }
  }
);

// --- Tool 4: device_configure ---
server.tool(
  "device_configure",
  "Configure the emulator device state. Set GPS location, network conditions, battery level, orientation, locale, dark mode, timezone, and more.",
  {
    session_id: z.string().describe("Session or instance ID"),
    setting: z.enum(["gps", "network", "battery", "orientation", "dark_mode", "locale", "timezone"]).describe("Setting to change"),
    latitude: z.number().optional().describe("GPS latitude"),
    longitude: z.number().optional().describe("GPS longitude"),
    network_profile: z.enum(["2g", "3g", "4g", "5g", "wifi_slow", "wifi_fast", "offline"]).optional(),
    battery_level: z.number().optional().describe("Battery level 0-100"),
    rotation: z.number().optional().describe("0=portrait, 1=landscape left, 2=reverse, 3=landscape right"),
    dark: z.boolean().optional(),
    locale: z.string().optional().describe("e.g. en-US, ja-JP, hi-IN"),
    timezone: z.string().optional().describe("e.g. America/New_York, Asia/Tokyo"),
  },
  async (args) => {
    const id = args.session_id;
    switch (args.setting) {
      case "gps": return result(await farm.setGPS(id, args.latitude || 0, args.longitude || 0));
      case "network": return result(await farm.setNetwork(id, args.network_profile || '4g'));
      case "battery": return result(await farm.setBattery(id, args.battery_level || 100));
      case "orientation": return result(await farm.setOrientation(id, args.rotation || 0));
      case "dark_mode": return result(await farm.setDarkMode(id, args.dark ?? false));
      case "locale": return result(await farm.setLocale(id, args.locale || 'en-US'));
      case "timezone": return result(await farm.setLocale(id, args.timezone || 'America/Los_Angeles'));
    }
  }
);

// --- Tool 5: app_manage ---
server.tool(
  "app_manage",
  "Manage apps on the emulator. Install APK, uninstall, launch, force-stop, clear data, or open deep links.",
  {
    session_id: z.string().describe("Session or instance ID"),
    action: z.enum(["install", "uninstall", "launch", "force_stop", "clear_data", "deeplink"]).describe("Action"),
    package_name: z.string().optional().describe("Android package name (e.g. com.example.app)"),
    apk_path: z.string().optional().describe("Local path to APK file (for install)"),
    url: z.string().optional().describe("URL or deep link (for deeplink)"),
  },
  async ({ session_id, action, package_name, apk_path, url }) => {
    switch (action) {
      case "install": return result(await farm.installApp(session_id, apk_path || ''));
      case "uninstall": return result(await farm.uninstallApp(session_id, package_name || ''));
      case "launch": return result(await farm.launchApp(session_id, package_name || ''));
      case "force_stop": return result(await farm.forceStop(session_id, package_name || ''));
      case "clear_data": return result(await farm.clearData(session_id, package_name || ''));
      case "deeplink": return result(await farm.openDeeplink(session_id, url || ''));
    }
  }
);

// --- Tool 6: device_debug ---
server.tool(
  "device_debug",
  "Debug the emulator. Get logcat output, run raw ADB commands, check for crashes, or get package info.",
  {
    session_id: z.string().describe("Session or instance ID"),
    action: z.enum(["logcat", "adb", "crash_check", "package_info"]).describe("Debug action"),
    command: z.string().optional().describe("ADB shell command (for adb action)"),
    package_name: z.string().optional().describe("Package to check (for crash_check, package_info)"),
    lines: z.number().optional().describe("Number of logcat lines (default 100)"),
  },
  async ({ session_id, action, command, package_name, lines }) => {
    switch (action) {
      case "logcat": {
        const r = await farm.getLogs(session_id, lines || 100);
        return { content: [{ type: "text", text: r.text || JSON.stringify(r) }] };
      }
      case "adb":
        return result(await farm.execADB(session_id, command || 'echo ok'));
      case "crash_check":
        return result(await farm.checkCrash(session_id, package_name || ''));
      case "package_info": {
        const r = await farm.execADB(session_id, `dumpsys package ${package_name} | head -20`);
        return result(r);
      }
    }
  }
);

// --- Tool 7: device_record ---
server.tool(
  "device_record",
  "Record video or capture network traffic on the emulator.",
  {
    session_id: z.string().describe("Session or instance ID"),
    action: z.enum(["start_video", "stop_video", "start_har", "stop_har"]).describe("Recording action"),
  },
  async ({ session_id, action }) => {
    switch (action) {
      case "start_video": return result(await farm.startRecording(session_id));
      case "stop_video": return result(await farm.stopRecording(session_id));
      case "start_har": return result(await farm.startHAR(session_id));
      case "stop_har": return result(await farm.stopHAR(session_id));
    }
  }
);

// Helper
function result(data: any) {
  return { content: [{ type: "text" as const, text: JSON.stringify(data, null, 2) }] };
}

// Start
async function main() {
  const transport = new StdioServerTransport();
  await server.connect(transport);
  console.error("prodigy-core MCP server running (drizz-farm)");
}

main().catch(console.error);
