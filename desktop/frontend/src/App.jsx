import { useCallback, useEffect, useState } from 'react';
import { DashboardPage } from './components/DashboardPage.jsx';
import { LogsPage } from './components/LogsPage.jsx';
import { RunsPage } from './components/RunsPage.jsx';
import { SettingsPage } from './components/SettingsPage.jsx';
import { AccountsPage } from './components/AccountsPage.jsx';
import { APIKeysPage } from './components/APIKeysPage.jsx';
import { ModelsPage } from './components/ModelsPage.jsx';
import { GatewayPage } from './components/GatewayPage.jsx';
import { GatewayLogsPage } from './components/GatewayLogsPage.jsx';
import { Sidebar } from './components/Sidebar.jsx';
import { StatusBar } from './components/StatusBar.jsx';
import { TopBar } from './components/TopBar.jsx';
import {
	bootstrap, checkGatewayAccounts, clearGatewayRequestLogs, createAPIKey, deleteAPIKey, deleteGatewayAccount,
	gatewayStatus, getAPIKeySecret,
	getDashboard, getSettings, importGatewayAccounts, listAPIKeys, listGatewayAccounts, listGatewayModels, listGatewayRequestLogs,
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
	const [gatewayLogs, setGatewayLogs] = useState([]);
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

	const refreshGatewayLogs = useCallback(async () => {
		const logs = await listGatewayRequestLogs();
		setGatewayLogs(logs || []);
	}, []);

	useEffect(() => {
		if (view !== 'gateway-logs') return undefined;
		let active = true;
		const poll = () => listGatewayRequestLogs().then((logs) => { if (active) setGatewayLogs(logs || []); }).catch((err) => { if (active) setError(err?.message || String(err)); });
		poll();
		const timer = window.setInterval(poll, 1000);
		return () => { active = false; window.clearInterval(timer); };
	}, [view]);

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

  async function handleSaveSettings(success) {
    const successMessage = typeof success === 'string' ? success : '配置已经保存';
    setBusy(true);
    try {
      await saveSettings(settings);
      await refreshGateway();
		setNotice(successMessage);
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

  async function handleImportAccounts() {
    setBusy(true);
    try {
      const result = await importGatewayAccounts();
      if (!result.selected_files) return result;
      await refreshGateway();
      setNotice(result.failed_files ? `导入完成：${result.imported_accounts} 个账号，${result.failed_files} 个文件失败` : `已导入 ${result.imported_accounts} 个账号`);
      setError('');
      return result;
    } catch (err) {
      setError(err?.message || String(err));
      return null;
    } finally {
      setBusy(false);
    }
  }

  async function handleCheckAccounts() {
    setBusy(true);
    try {
      const result = await checkGatewayAccounts();
      await refreshGateway();
      setNotice(`测活完成：${result.healthy} 个正常，${result.unhealthy} 个异常`);
      setError('');
      return result;
    } catch (err) {
      setError(err?.message || String(err));
      return null;
    } finally {
      setBusy(false);
    }
  }

  async function handleCreateKey(name) {
    setBusy(true);
    try { const result = await createAPIKey(name); setCreatedSecret(result.secret); await refreshGateway(); }
    catch (err) { setError(err?.message || String(err)); }
    finally { setBusy(false); }
  }

  async function handleCopyKey(id) {
		setBusy(true);
		try {
			const secret = await getAPIKeySecret(id);
			if (!navigator.clipboard?.writeText) throw new Error('clipboard unavailable');
			await navigator.clipboard.writeText(secret);
			setNotice('完整 API Key 已复制');
		} catch (err) {
			try { setCreatedSecret(await getAPIKeySecret(id)); }
			catch { setError(err?.message || String(err)); }
		} finally { setBusy(false); }
	}

	async function handleClearGatewayLogs() {
		await gatewayAction(clearGatewayRequestLogs, '网关日志已清空');
		setGatewayLogs([]);
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
          {view === 'gateway' ? <GatewayPage busy={busy} onSave={handleSaveSettings} setSettings={setSettings} settings={settings} status={gateway.status} /> : null}
			{view === 'gateway-logs' ? <GatewayLogsPage busy={busy} logs={gatewayLogs} onClear={handleClearGatewayLogs} onRefresh={refreshGatewayLogs} /> : null}
          {view === 'accounts' ? <AccountsPage accounts={gateway.accounts} busy={busy} onCheck={handleCheckAccounts} onDelete={(id) => gatewayAction(() => deleteGatewayAccount(id), '账号已删除')} onImport={handleImportAccounts} onRefresh={refreshGateway} onSaveHealth={() => handleSaveSettings('账号测活设置已保存')} onUpdate={(account) => gatewayAction(() => updateGatewayAccount(account), '账号设置已保存')} setSettings={setSettings} settings={settings} /> : null}
          {view === 'models' ? <ModelsPage models={gateway.models} /> : null}
			{view === 'keys' ? <APIKeysPage apiKeys={gateway.keys} busy={busy} onCopy={handleCopyKey} onCreate={handleCreateKey} onDelete={(id) => gatewayAction(() => deleteAPIKey(id), 'API Key 已删除')} onToggle={(id, enabled) => gatewayAction(() => setAPIKeyEnabled(id, enabled), 'API Key 状态已更新')} /> : null}
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
