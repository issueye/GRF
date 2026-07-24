import {
  Activity, ChevronLeft, ChevronRight, Download, FileUp, LayoutGrid, RefreshCw,
  Save, Table2, Timer, Trash2, UserRound, Users, X,
} from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';

const pageSizes = [12, 24, 48];

export function AccountsPage({ accounts, busy, onCheck, onDelete, onExport, onImport, onRefresh, onSaveHealth, onUpdate, setSettings, settings }) {
  const [importResult, setImportResult] = useState(null);
  const [exportResult, setExportResult] = useState(null);
  const [checkResult, setCheckResult] = useState(null);
  const [viewMode, setViewMode] = useState(() => window.localStorage.getItem('grf.accounts.view') === 'cards' ? 'cards' : 'table');
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(12);
  const pageCount = Math.max(1, Math.ceil(accounts.length / pageSize));
  const visibleAccounts = useMemo(() => accounts.slice((page - 1) * pageSize, page * pageSize), [accounts, page, pageSize]);

  useEffect(() => setPage((current) => Math.min(current, pageCount)), [pageCount]);
  useEffect(() => window.localStorage.setItem('grf.accounts.view', viewMode), [viewMode]);

  const runImport = async () => {
    const result = await onImport();
    if (result?.selected_files) setImportResult(result);
  };
  const runExport = async () => {
    const result = await onExport();
    if (result?.total_accounts) setExportResult(result);
  };
  const patchHealth = (key, value) => setSettings((current) => ({ ...current, [key]: value }));
  const runCheck = async () => {
    const result = await onCheck();
    if (result) setCheckResult(result);
  };
  const changeView = (mode) => {
    setViewMode(mode);
    setPage(1);
  };

  return (
    <section className="page" aria-labelledby="accounts-title">
      <div className="page-heading"><h1 id="accounts-title">账号</h1><div className="heading-actions"><button className="button button-primary" disabled={busy} onClick={runImport} type="button"><FileUp size={14} /> 导入 CPA</button><button className="button button-secondary" disabled={busy || !accounts.length} onClick={runExport} type="button"><Download size={14} /> 导出 CPA</button><button className="button button-secondary" disabled={busy} onClick={onRefresh} type="button"><RefreshCw size={14} /> 刷新</button></div></div>
      <form className="account-health-panel" onSubmit={(event) => { event.preventDefault(); onSaveHealth(); }}>
        <div className="account-health-title"><Timer size={17} /><span><strong>定时测活</strong><small>通过 Build 模型目录检查已启用账号，不消耗对话额度</small></span></div>
        <label className="switch-row account-health-switch">
          <input checked={Boolean(settings?.api_account_health_enabled)} disabled={!settings} onChange={(event) => patchHealth('api_account_health_enabled', event.target.checked)} type="checkbox" />
        </label>
        <label className="account-health-interval"><span>间隔（分钟）</span><input disabled={!settings} max="1440" min="5" onChange={(event) => patchHealth('api_account_health_interval_minutes', Number(event.target.value) || 5)} type="number" value={settings?.api_account_health_interval_minutes || 30} /></label>
        <button className="button button-secondary" disabled={busy || !settings} type="submit"><Save size={14} /> 保存</button>
        <button className="button button-secondary" disabled={busy || !accounts.length} onClick={runCheck} type="button"><Activity size={14} /> 立即测活</button>
      </form>
      {importResult ? <div className={importResult.failed_files ? 'import-summary has-errors' : 'import-summary'}><FileUp size={16} /><div><strong>已导入 {importResult.imported_accounts} 个账号</strong><span>{importResult.successful_files} 个文件成功{importResult.failed_files ? `，${importResult.failed_files} 个文件失败` : ''}</span>{importResult.failures?.length ? <ul>{importResult.failures.slice(0, 5).map((failure, index) => <li key={`${failure.file}-${index}`}><b>{failure.file}</b><span>{failure.error}</span></li>)}</ul> : null}</div><button className="icon-button" onClick={() => setImportResult(null)} title="关闭导入结果" type="button"><X size={14} /></button></div> : null}
      {exportResult ? <div className={exportResult.failed_accounts ? 'import-summary has-errors' : 'import-summary'}><Download size={16} /><div><strong>已导出 {exportResult.exported_accounts} 个账号</strong><span>{exportResult.path ? `${exportResult.path}` : ''}{exportResult.failed_accounts ? `，${exportResult.failed_accounts} 个账号失败` : ''}</span>{exportResult.failures?.length ? <ul>{exportResult.failures.slice(0, 5).map((failure, index) => <li key={`${failure.account}-${index}`}><b>{failure.account}</b><span>{failure.error}</span></li>)}</ul> : null}</div><button className="icon-button" onClick={() => setExportResult(null)} title="关闭导出结果" type="button"><X size={14} /></button></div> : null}
      {checkResult ? <div className={checkResult.unhealthy ? 'health-summary has-errors' : 'health-summary'}><Activity size={16} /><span><strong>测活完成</strong><small>{checkResult.checked} 个账号：{checkResult.healthy} 个正常，{checkResult.unhealthy} 个异常</small></span><button className="icon-button" onClick={() => setCheckResult(null)} title="关闭测活结果" type="button"><X size={14} /></button></div> : null}

      <div className="accounts-browser-toolbar">
        <div className="account-count"><Users size={15} /><strong>{accounts.length}</strong><span>个账号</span></div>
        <div className="segmented-control account-view-switch" aria-label="账号显示方式" role="group">
          <button aria-pressed={viewMode === 'table'} className={viewMode === 'table' ? 'active' : ''} onClick={() => changeView('table')} title="表格视图" type="button"><Table2 size={14} /><span>表格</span></button>
          <button aria-pressed={viewMode === 'cards'} className={viewMode === 'cards' ? 'active' : ''} onClick={() => changeView('cards')} title="卡片视图" type="button"><LayoutGrid size={14} /><span>卡片</span></button>
        </div>
      </div>

      {!accounts.length ? <div className="empty-state table-shell"><Users size={24} /><strong>暂无可用账号</strong><span>完成一次注册后，凭据将自动加密导入。</span></div> : (
        <>
          {viewMode === 'table' ? (
            <div className="gateway-table accounts-table">
              <div className="gateway-table-head"><span>账号</span><span>状态</span><span>测活</span><span>并发</span><span>操作</span></div>
              <div className="gateway-table-body">{visibleAccounts.map((account) => <AccountRow account={account} key={account.id} onDelete={onDelete} onUpdate={onUpdate} />)}</div>
            </div>
          ) : (
            <div className="account-card-grid">{visibleAccounts.map((account) => <AccountCard account={account} key={account.id} onDelete={onDelete} onUpdate={onUpdate} />)}</div>
          )}
          <AccountPagination count={accounts.length} page={page} pageCount={pageCount} pageSize={pageSize} setPage={setPage} setPageSize={setPageSize} />
        </>
      )}
    </section>
  );
}

function useAccountDraft(account) {
  const [draft, setDraft] = useState({ enabled: account.enabled, max_concurrent: account.max_concurrent });
  useEffect(() => setDraft({ enabled: account.enabled, max_concurrent: account.max_concurrent }), [account.enabled, account.max_concurrent]);
  return [draft, setDraft];
}

function accountState(account, draft) {
  const health = account.health_status || 'unknown';
  const healthText = health === 'healthy' ? '正常' : health === 'unhealthy' ? '异常' : '未检测';
  const healthDetail = account.last_checked_at ? `${new Date(account.last_checked_at).toLocaleString()} · ${account.health_latency_ms || 0} ms` : '等待首次检测';
  const unavailable = draft.enabled && (account.auth_status !== 'active' || health === 'unhealthy');
  const statusText = !draft.enabled ? '已停用' : account.auth_status !== 'active' ? '需认证' : health === 'unhealthy' ? '异常' : '已启用';
  return { health, healthText, healthDetail, unavailable, statusText };
}

function AccountRow({ account, onDelete, onUpdate }) {
  const [draft, setDraft] = useAccountDraft(account);
  const state = accountState(account, draft);
  const save = () => onUpdate({ id: account.id, name: account.name || '', ...draft });
  return <div className="gateway-table-row"><AccountIdentity account={account} /><label className={state.unavailable ? 'compact-toggle is-unavailable' : 'compact-toggle'}><input checked={draft.enabled} onChange={(event) => setDraft((value) => ({ ...value, enabled: event.target.checked }))} type="checkbox" /><span>{state.statusText}</span></label><HealthState account={account} state={state} /><input aria-label={`${account.name || account.email || account.id} 最大并发`} className="concurrency-input" max="64" min="1" onChange={(event) => setDraft((value) => ({ ...value, max_concurrent: Number(event.target.value) || 1 }))} type="number" value={draft.max_concurrent} /><AccountActions account={account} onDelete={onDelete} onSave={save} /></div>;
}

function AccountCard({ account, onDelete, onUpdate }) {
  const [draft, setDraft] = useAccountDraft(account);
  const state = accountState(account, draft);
  const save = () => onUpdate({ id: account.id, name: account.name || '', ...draft });
  return (
    <article className="account-card">
      <header><span className="account-card-icon"><UserRound size={17} /></span><AccountIdentity account={account} /></header>
      <div className="account-card-status"><label className={state.unavailable ? 'compact-toggle is-unavailable' : 'compact-toggle'}><input checked={draft.enabled} onChange={(event) => setDraft((value) => ({ ...value, enabled: event.target.checked }))} type="checkbox" /><span>{state.statusText}</span></label><HealthState account={account} state={state} /></div>
      <footer><label><span>最大并发</span><input className="concurrency-input" max="64" min="1" onChange={(event) => setDraft((value) => ({ ...value, max_concurrent: Number(event.target.value) || 1 }))} type="number" value={draft.max_concurrent} /></label><AccountActions account={account} onDelete={onDelete} onSave={save} /></footer>
    </article>
  );
}

function AccountIdentity({ account }) {
  return <div className="account-identity"><strong>{account.name || account.email || `账号 ${account.id}`}</strong><small>{account.email || account.user_id || account.provider}</small></div>;
}

function HealthState({ account, state }) {
  return <div className={`account-health-state is-${state.health}`} title={account.health_error || state.healthDetail}><span><i />{state.healthText}</span><small>{account.health_error || state.healthDetail}</small></div>;
}

function AccountActions({ account, onDelete, onSave }) {
  return <div className="row-actions"><button className="icon-button bordered" onClick={onSave} title="保存账号" type="button"><Save size={14} /></button><button className="icon-button bordered danger-icon" onClick={() => onDelete(account.id)} title="删除账号" type="button"><Trash2 size={14} /></button></div>;
}

function AccountPagination({ count, page, pageCount, pageSize, setPage, setPageSize }) {
  const first = (page - 1) * pageSize + 1;
  const last = Math.min(page * pageSize, count);
  const firstPageButton = Math.min(Math.max(1, page - 2), Math.max(1, pageCount - 4));
  const pageButtons = Array.from({ length: Math.min(5, pageCount) }, (_, index) => firstPageButton + index);
  return (
    <nav className="account-pagination" aria-label="账号分页">
      <span className="pagination-range">{first}-{last} / {count}</span>
      <div className="pagination-pages">
        <label>
          <span style={{ marginRight: 4 }}>每页</span>
          <select aria-label="每页账号数" onChange={(event) => { setPageSize(Number(event.target.value)); setPage(1); }} value={pageSize}>{pageSizes.map((size) => <option key={size} value={size}>{size}</option>)}</select>
        </label>
        <button aria-label="上一页" className="icon-button bordered" disabled={page === 1} onClick={() => setPage((current) => current - 1)} title="上一页" type="button"><ChevronLeft size={15} /></button>
        {pageButtons.map((number) => <button aria-current={number === page ? 'page' : undefined} className={number === page ? 'page-button active' : 'page-button'} key={number} onClick={() => setPage(number)} type="button">{number}</button>)}
        <button aria-label="下一页" className="icon-button bordered" disabled={page === pageCount} onClick={() => setPage((current) => current + 1)} title="下一页" type="button"><ChevronRight size={15} /></button>
      </div>
    </nav>
  );
}
