import { Call, Window } from '@wailsio/runtime';

const demoDashboard = {
  status: 'stopped',
  running: false,
  run_id: '',
  target: 10,
  done: 0,
  sso_count: 0,
  oauth_count: 0,
  fail_count: 0,
  phase: 'idle',
  phase_detail: '等待启动',
  workers: { s: 2, p: 0, c: 0, oauth: 0 },
  pid: 0,
  rate_per_min: 0,
};

const demoSettings = {
  email_mode: 'tempmail',
  email_domain: '',
  email_api: 'http://127.0.0.1:8080',
  register_proxy: 'http://127.0.0.1:40080',
  clearance_enabled: true,
  flaresolverr_url: 'http://127.0.0.1:8191',
  turnstile_provider: 'browser',
  lite_solver_url: 'http://127.0.0.1:5072',
  cpa_upload_enabled: false,
  cpa_management_base: 'http://localhost:8317/v0/management',
};

function hasNativeRuntime() {
  return Boolean(window?._wails?.environment);
}

async function invoke(method, ...args) {
  return Call.ByName(`github.com/grok-free-register/grok-reg/desktop/app.App.${method}`, ...args);
}

export async function bootstrap() {
  if (!hasNativeRuntime()) {
    return {
      name: 'GRF', version: 'preview', platform: 'browser',
      data_root: 'C:\\Users\\preview\\.gtr',
      config_path: 'C:\\Users\\preview\\.gtr\\config.env',
      output_root: 'C:\\Users\\preview\\.gtr\\outputs',
      chrome_path: 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
    };
  }
  return invoke('Bootstrap');
}

export async function getDashboard() {
  return hasNativeRuntime() ? invoke('GetDashboard') : demoDashboard;
}

export async function startRegistration(request) {
  if (!hasNativeRuntime()) {
    demoDashboard.running = true;
    demoDashboard.status = 'running';
    demoDashboard.target = request.target;
    demoDashboard.workers.s = request.threads;
    demoDashboard.phase = 'clearance';
    demoDashboard.phase_detail = '正在预热网络环境';
    return { pid: 24816, run_id: 'preview-run' };
  }
  return invoke('Start', request);
}

export async function stopRegistration() {
  if (!hasNativeRuntime()) {
    demoDashboard.running = false;
    demoDashboard.status = 'stopped';
    demoDashboard.phase = 'idle';
    demoDashboard.phase_detail = '已停止';
    return;
  }
  return invoke('Stop');
}

export async function tailLog() {
  if (!hasNativeRuntime()) {
    return '[21:42:03] INFO 等待任务启动\n[21:42:03] INFO Chrome 已就绪\n';
  }
  return invoke('TailLog', 131072);
}

export async function listRuns() {
  if (!hasNativeRuntime()) {
    return [
      { id: '20260722-204512', path: 'C:\\Users\\preview\\.gtr\\outputs\\20260722-204512', updated_at: '2026-07-22T20:49:08+08:00', cpa_count: 8, sso_count: 8 },
      { id: '20260722-193104', path: 'C:\\Users\\preview\\.gtr\\outputs\\20260722-193104', updated_at: '2026-07-22T19:36:42+08:00', cpa_count: 5, sso_count: 6 },
    ];
  }
  try {
    return await invoke('ListRuns', 20);
  } catch (error) {
    // Older desktop binaries did not expose ListRuns. Keep the rest of the
    // console usable when a cached frontend is paired with such a backend.
    if (String(error?.message || error).includes('unknown bound method name')) {
      return [];
    }
    throw error;
  }
}

export async function getSettings() {
  return hasNativeRuntime() ? invoke('GetSettings') : demoSettings;
}

export async function saveSettings(settings) {
  if (!hasNativeRuntime()) {
    Object.assign(demoSettings, settings);
    return;
  }
  return invoke('SaveSettings', settings);
}

export async function openConfig() {
  return hasNativeRuntime() ? invoke('OpenConfig') : undefined;
}

export async function openPath(path) {
  return hasNativeRuntime() ? invoke('OpenPath', path) : undefined;
}

export const windowActions = {
  minimise: () => Window.Minimise().catch(() => {}),
  toggleMaximise: () => Window.ToggleMaximise().catch(() => {}),
  close: () => Window.Close().catch(() => {}),
};
