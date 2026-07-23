import { useCallback, useEffect, useState } from 'react';
import { DashboardPage } from './components/DashboardPage.jsx';
import { LogsPage } from './components/LogsPage.jsx';
import { RunsPage } from './components/RunsPage.jsx';
import { SettingsPage } from './components/SettingsPage.jsx';
import { AccountsPage } from './components/AccountsPage.jsx';
import { APIKeysPage } from './components/APIKeysPage.jsx';
import { ModelsPage } from './components/ModelsPage.jsx';
import { Sidebar } from './components/Sidebar.jsx';
import { StatusBar } from './components/StatusBar.jsx';
import { TopBar } from './components/TopBar.jsx';
import {
  bootstrap, createAPIKey, deleteAPIKey, deleteGatewayAccount, gatewayStatus,
  getDashboard, getSettings, listAPIKeys, listGatewayAccounts, listGatewayModels,
  listRuns, openConfig, openPath, saveSettings, setAPIKeyEnabled,
  startRegistration, stopRegistration, tailLog, updateGatewayAccount,
} from './lib/native.js';

const emptyDashboard = {
  status: 'stopped', running: false, target: 0, done: 0, sso_count: 0,
  oauth_count: 0, fail_count: 0, phase: 'idle', phase_detail: '正在读取状态',
  workers: { s: 0, p: 0, c: 0, oauth: 0 }, pid: 0,
};

export function App() {
  const [view, setView] = useState('overview');
  const [info, setInfo] = useState(null);
  const [dashboard, setDashboard] = useState(emptyDashboard);
  const [log, setLog] = useState('');
  const [runs, setRuns] = useState([]);
  const [settings, setSettings] = useState(null);
  const [gateway, setGateway] = useState({ status: {}, accounts: [], models: [], keys: [] });
  const [createdSecret, setCreatedSecret] = useState('');
  const [target, setTarget] = useState(10);
  const [threads, setThreads] = useState(2);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [confirmStop, setConfirmStop] = useState(false);

  const refresh = useCallback(async (quiet = false) => {
    if (!quiet) setBusy(true);
    try {
      const [nextDashboard, nextLog, nextRuns] = await Promise.all([getDashboard(), tailLog(), listRuns()]);
      setDashboard(nextDashboard);
      setLog(nextLog);
      setRuns(nextRuns || []);
      setError('');
    } catch (err) {
      setError(err?.message || String(err));
    } finally {
      if (!quiet) setBusy(false);
    }
  }, []);

  const refreshGateway = useCallback(async () => {
    const [status, accounts, models, keys] = await Promise.all([gatewayStatus(), listGatewayAccounts(), listGatewayModels(), listAPIKeys()]);
    setGateway({ status, accounts: accounts || [], models: models || [], keys: keys || [] });
  }, []);

  useEffect(() => {
    Promise.all([bootstrap(), getSettings(), refreshGateway()])
      .then(([nextInfo, nextSettings]) => {
        setInfo(nextInfo);
        setSettings(nextSettings);
      })
      .catch((err) => setError(err?.message || String(err)));
    refresh();
  }, [refresh, refreshGateway]);

  useEffect(() => {
    const timer = window.setInterval(() => refresh(true), dashboard.running ? 1200 : 3500);
    return () => window.clearInterval(timer);
  }, [dashboard.running, refresh]);

  async function handleStart() {
    setBusy(true);
    try {
      await startRegistration({ target, threads });
      setNotice('任务已经在后台启动');
      await refresh(true);
    } catch (err) {
      setError(err?.message || String(err));
    } finally {
      setBusy(false);
    }
  }

  async function handleStop() {
    setConfirmStop(false);
    setBusy(true);
    try {
      await stopRegistration();
      setNotice('停止请求已完成');
      await refresh(true);
    } catch (err) {
      setError(err?.message || String(err));
    } finally {
      setBusy(false);
    }
  }

  async function handleSaveSettings() {
    setBusy(true);
    try {
      await saveSettings(settings);
      await refreshGateway();
      setNotice('配置已经保存');
    } catch (err) {
      setError(err?.message || String(err));
    } finally {
      setBusy(false);
    }
  }

  async function gatewayAction(action, success) {
    setBusy(true);
    try { await action(); await refreshGateway(); setNotice(success); setError(''); }
    catch (err) { setError(err?.message || String(err)); }
    finally { setBusy(false); }
  }

  async function handleCreateKey(name) {
    setBusy(true);
    try { const result = await createAPIKey(name); setCreatedSecret(result.secret); await refreshGateway(); }
    catch (err) { setError(err?.message || String(err)); }
    finally { setBusy(false); }
  }

  return (
    <div className="app-shell">
      <TopBar busy={busy} onRefresh={() => refresh()} running={dashboard.running} />
      <div className="workspace">
        <Sidebar active={view} bootstrapInfo={info} onChange={setView} />
        <main className="main-content">
          {view === 'overview' ? <DashboardPage busy={busy} dashboard={dashboard} log={log} onOpenOutput={openPath} onStart={handleStart} onStop={() => setConfirmStop(true)} setTarget={setTarget} setThreads={setThreads} target={target} threads={threads} /> : null}
          {view === 'logs' ? <LogsPage busy={busy} log={log} onOpenPath={openPath} onRefresh={() => refresh()} path={dashboard.log_path} /> : null}
          {view === 'runs' ? <RunsPage onOpenPath={openPath} runs={runs} /> : null}
          {view === 'accounts' ? <AccountsPage accounts={gateway.accounts} busy={busy} onDelete={(id) => gatewayAction(() => deleteGatewayAccount(id), '账号已删除')} onRefresh={refreshGateway} onUpdate={(account) => gatewayAction(() => updateGatewayAccount(account), '账号设置已保存')} /> : null}
          {view === 'models' ? <ModelsPage models={gateway.models} /> : null}
          {view === 'keys' ? <APIKeysPage apiKeys={gateway.keys} busy={busy} onCreate={handleCreateKey} onDelete={(id) => gatewayAction(() => deleteAPIKey(id), 'API Key 已删除')} onToggle={(id, enabled) => gatewayAction(() => setAPIKeyEnabled(id, enabled), 'API Key 状态已更新')} /> : null}
          {view === 'settings' ? <SettingsPage busy={busy} configPath={info?.config_path || ''} onOpenConfig={openConfig} onSave={handleSaveSettings} setSettings={setSettings} settings={settings} /> : null}
        </main>
      </div>
      <StatusBar bootstrapInfo={info} dashboard={dashboard} />

      {error ? <div className="toast toast-error" role="alert"><strong>操作未完成</strong><span>{error}</span><button onClick={() => setError('')} type="button">关闭</button></div> : null}
      {notice ? <div className="toast toast-success" role="status"><strong>已完成</strong><span>{notice}</span><button onClick={() => setNotice('')} type="button">关闭</button></div> : null}
      {confirmStop ? (
        <div className="dialog-backdrop" role="presentation" onMouseDown={() => setConfirmStop(false)}>
          <div aria-labelledby="stop-dialog-title" aria-modal="true" className="dialog" onMouseDown={(event) => event.stopPropagation()} role="dialog">
            <span className="dialog-mark">!</span><h2 id="stop-dialog-title">停止当前任务？</h2><p>系统会先请求后台 worker 优雅退出，超时后才会强制结束。已写入的产物不会删除。</p>
            <div><button className="button button-secondary" onClick={() => setConfirmStop(false)} type="button">继续运行</button><button autoFocus className="button button-danger" onClick={handleStop} type="button">确认停止</button></div>
          </div>
        </div>
      ) : null}
      {createdSecret ? <div className="dialog-backdrop"><div aria-modal="true" className="dialog secret-dialog" role="dialog"><span className="dialog-key">API KEY</span><h2>密钥已创建</h2><p>完整密钥只显示这一次。关闭后无法再次查看。</p><code>{createdSecret}</code><div><button className="button button-secondary" onClick={() => navigator.clipboard?.writeText(createdSecret)} type="button">复制</button><button className="button button-primary" onClick={() => setCreatedSecret('')} type="button">完成</button></div></div></div> : null}
    </div>
  );
}
