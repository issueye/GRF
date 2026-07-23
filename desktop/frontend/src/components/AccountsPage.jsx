import { Activity, FileUp, RefreshCw, Save, Timer, Trash2, Users, X } from 'lucide-react';
import { useEffect, useState } from 'react';

export function AccountsPage({ accounts, busy, onCheck, onDelete, onImport, onRefresh, onSaveHealth, onUpdate, setSettings, settings }) {
  const [importResult, setImportResult] = useState(null);
  const [checkResult, setCheckResult] = useState(null);
  const runImport = async () => {
    const result = await onImport();
    if (result?.selected_files) setImportResult(result);
  };
  const patchHealth = (key, value) => setSettings((current) => ({ ...current, [key]: value }));
  const runCheck = async () => {
    const result = await onCheck();
    if (result) setCheckResult(result);
  };
  return (
    <section className="page" aria-labelledby="accounts-title">
      <div className="page-heading"><h1 id="accounts-title">账号</h1><div className="heading-actions"><button className="button button-primary" disabled={busy} onClick={runImport} type="button"><FileUp size={14} /> 导入 CPA</button><button className="button button-secondary" disabled={busy} onClick={onRefresh} type="button"><RefreshCw size={14} /> 刷新</button></div></div>
      <form className="account-health-panel" onSubmit={(event) => { event.preventDefault(); onSaveHealth(); }}>
        <div className="account-health-title"><Timer size={17} /><span><strong>定时测活</strong><small>通过 Build 模型目录检查已启用账号，不消耗对话额度</small></span></div>
        <label className="switch-row account-health-switch"><span><strong>{settings?.api_account_health_enabled ? '已开启' : '已关闭'}</strong><small>保存后立即生效</small></span><input checked={Boolean(settings?.api_account_health_enabled)} disabled={!settings} onChange={(event) => patchHealth('api_account_health_enabled', event.target.checked)} type="checkbox" /></label>
        <label className="account-health-interval"><span>间隔（分钟）</span><input disabled={!settings} max="1440" min="5" onChange={(event) => patchHealth('api_account_health_interval_minutes', Number(event.target.value) || 5)} type="number" value={settings?.api_account_health_interval_minutes || 30} /></label>
        <button className="button button-secondary" disabled={busy || !settings} type="submit"><Save size={14} /> 保存</button>
        <button className="button button-secondary" disabled={busy || !accounts.length} onClick={runCheck} type="button"><Activity size={14} /> 立即测活</button>
      </form>
      {importResult ? <div className={importResult.failed_files ? 'import-summary has-errors' : 'import-summary'}><FileUp size={16} /><div><strong>已导入 {importResult.imported_accounts} 个账号</strong><span>{importResult.successful_files} 个文件成功{importResult.failed_files ? `，${importResult.failed_files} 个文件失败` : ''}</span>{importResult.failures?.length ? <ul>{importResult.failures.slice(0, 5).map((failure, index) => <li key={`${failure.file}-${index}`}><b>{failure.file}</b><span>{failure.error}</span></li>)}</ul> : null}</div><button className="icon-button" onClick={() => setImportResult(null)} title="关闭导入结果" type="button"><X size={14} /></button></div> : null}
      {checkResult ? <div className={checkResult.unhealthy ? 'health-summary has-errors' : 'health-summary'}><Activity size={16} /><span><strong>测活完成</strong><small>{checkResult.checked} 个账号：{checkResult.healthy} 个正常，{checkResult.unhealthy} 个异常</small></span><button className="icon-button" onClick={() => setCheckResult(null)} title="关闭测活结果" type="button"><X size={14} /></button></div> : null}
      {!accounts.length ? <div className="empty-state table-shell"><Users size={24} /><strong>暂无可用账号</strong><span>完成一次注册后，凭据将自动加密导入。</span></div> : (
        <div className="gateway-table accounts-table">
          <div className="gateway-table-head">
            <span>账号</span>
            <span>状态</span>
            <span>测活</span>
            <span>并发</span>
            <span>操作</span>
          </div>
          <div className="gateway-table-body">
            {accounts.map((account) => <AccountRow account={account} key={account.id} onDelete={onDelete} onUpdate={onUpdate} />)}
          </div>
        </div>
      )}
    </section>
  );
}

function AccountRow({ account, onDelete, onUpdate }) {
	const [draft, setDraft] = useState({ enabled: account.enabled, max_concurrent: account.max_concurrent });
	useEffect(() => setDraft({ enabled: account.enabled, max_concurrent: account.max_concurrent }), [account.enabled, account.max_concurrent]);
	const save = () => onUpdate({ id: account.id, name: account.name || '', ...draft });
  const health = account.health_status || 'unknown';
  const healthText = health === 'healthy' ? '正常' : health === 'unhealthy' ? '异常' : '未检测';
  const healthDetail = account.last_checked_at ? `${new Date(account.last_checked_at).toLocaleString()} · ${account.health_latency_ms || 0} ms` : '等待首次检测';
  const unavailable = draft.enabled && (account.auth_status !== 'active' || health === 'unhealthy');
  const statusText = !draft.enabled ? '已停用' : account.auth_status !== 'active' ? '需认证' : health === 'unhealthy' ? '异常' : '已启用';
  return <div className="gateway-table-row"><div><strong>{account.name || account.email || `账号 ${account.id}`}</strong><small>{account.email || account.user_id || account.provider}</small></div><label className={unavailable ? 'compact-toggle is-unavailable' : 'compact-toggle'}><input checked={draft.enabled} onChange={(e) => setDraft((value) => ({ ...value, enabled: e.target.checked }))} type="checkbox" /><span>{statusText}</span></label><div className={`account-health-state is-${health}`} title={account.health_error || healthDetail}><span><i />{healthText}</span><small>{account.health_error || healthDetail}</small></div><input className="concurrency-input" max="64" min="1" onChange={(e) => setDraft((value) => ({ ...value, max_concurrent: Number(e.target.value) || 1 }))} type="number" value={draft.max_concurrent} /><div className="row-actions"><button className="icon-button bordered" onClick={save} title="保存账号" type="button"><Save size={14} /></button><button className="icon-button bordered danger-icon" onClick={() => onDelete(account.id)} title="删除账号" type="button"><Trash2 size={14} /></button></div></div>;
}
