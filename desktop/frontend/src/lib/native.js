import { Call, Dialogs, Window } from '@wailsio/runtime';

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
  api_enabled: false,
  api_listen_host: '127.0.0.1',
  api_listen_port: 8000,
	api_stream_default: false,
};

const demoGateway = {
  status: { running: false, address: '' },
  accounts: [],
  models: [{ id: 'grok-4.5', object: 'model', owned_by: 'xai' }],
  keys: [],
	logs: [],
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
      data_root: 'C:\\Users\\preview\\.grf',
      config_path: 'C:\\Users\\preview\\.grf\\config.env',
      output_root: 'C:\\Users\\preview\\.grf\\outputs',
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
      { id: '20260722-204512', path: 'C:\\Users\\preview\\.grf\\outputs\\20260722-204512', updated_at: '2026-07-22T20:49:08+08:00', cpa_count: 8, sso_count: 8 },
      { id: '20260722-193104', path: 'C:\\Users\\preview\\.grf\\outputs\\20260722-193104', updated_at: '2026-07-22T19:36:42+08:00', cpa_count: 5, sso_count: 6 },
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

export async function gatewayStatus() {
  return hasNativeRuntime() ? invoke('GatewayStatus') : demoGateway.status;
}

export async function listGatewayRequestLogs() {
	return hasNativeRuntime() ? invoke('ListGatewayRequestLogs', 500) : demoGateway.logs;
}

export async function clearGatewayRequestLogs() {
	if (hasNativeRuntime()) return invoke('ClearGatewayRequestLogs');
	demoGateway.logs = [];
}

export async function listGatewayAccounts() {
  return hasNativeRuntime() ? invoke('ListGatewayAccounts') : demoGateway.accounts;
}

export async function importGatewayAccounts() {
  if (!hasNativeRuntime()) {
    return { selected_files: 0, successful_files: 0, failed_files: 0, imported_accounts: 0, failures: [] };
  }
  const paths = await Dialogs.OpenFile({
    Title: '选择 CPA 账号文件',
    Message: '可按住 Ctrl 或 Shift 多选 JSON 文件',
    ButtonText: '导入',
    CanChooseFiles: true,
    CanChooseDirectories: false,
    AllowsMultipleSelection: true,
    AllowsOtherFiletypes: false,
    Filters: [{ DisplayName: 'CPA JSON', Pattern: '*.json' }],
  });
  if (!paths.length) {
    return { selected_files: 0, successful_files: 0, failed_files: 0, imported_accounts: 0, failures: [] };
  }
  return invoke('ImportGatewayAccounts', paths);
}

export async function updateGatewayAccount(account) {
  if (hasNativeRuntime()) return invoke('UpdateGatewayAccount', account);
  const index = demoGateway.accounts.findIndex((item) => item.id === account.id);
  if (index >= 0) demoGateway.accounts[index] = { ...demoGateway.accounts[index], ...account };
}

export async function deleteGatewayAccount(id) {
  if (hasNativeRuntime()) return invoke('DeleteGatewayAccount', id);
  demoGateway.accounts = demoGateway.accounts.filter((item) => item.id !== id);
}

export async function listGatewayModels() {
  return hasNativeRuntime() ? invoke('ListGatewayModels') : demoGateway.models;
}

export async function listAPIKeys() {
  return hasNativeRuntime() ? invoke('ListAPIKeys') : demoGateway.keys;
}

export async function createAPIKey(name) {
  if (hasNativeRuntime()) return invoke('CreateAPIKey', name);
  const secret = `grf_preview_${Date.now()}`;
	const key = { id: Date.now(), name, prefix: secret.slice(0, 12), enabled: true, has_secret: true, created_at: new Date().toISOString(), _secret: secret };
  demoGateway.keys.unshift(key);
  return { key, secret };
}

export async function getAPIKeySecret(id) {
	if (hasNativeRuntime()) return invoke('GetAPIKeySecret', id);
	const key = demoGateway.keys.find((item) => item.id === id);
	if (!key?._secret) throw new Error('该密钥由旧版本创建，无法恢复');
	return key._secret;
}

export async function setAPIKeyEnabled(id, enabled) {
  if (hasNativeRuntime()) return invoke('SetAPIKeyEnabled', id, enabled);
  const key = demoGateway.keys.find((item) => item.id === id);
  if (key) key.enabled = enabled;
}

export async function deleteAPIKey(id) {
  if (hasNativeRuntime()) return invoke('DeleteAPIKey', id);
  demoGateway.keys = demoGateway.keys.filter((item) => item.id !== id);
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
