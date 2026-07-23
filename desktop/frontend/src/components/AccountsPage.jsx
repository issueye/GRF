import { FileUp, RefreshCw, Save, Trash2, Users, X } from 'lucide-react';
import { useEffect, useState } from 'react';

export function AccountsPage({ accounts, busy, onDelete, onImport, onRefresh, onUpdate }) {
  const [importResult, setImportResult] = useState(null);
  const runImport = async () => {
    const result = await onImport();
    if (result?.selected_files) setImportResult(result);
  };
  return (
    <section className="page" aria-labelledby="accounts-title">
      <div className="page-heading"><div><h1 id="accounts-title">账号</h1><p>注册成功的账号会自动入库，也可批量导入 CPA JSON。</p></div><div className="heading-actions"><button className="button button-primary" disabled={busy} onClick={runImport} type="button"><FileUp size={14} /> 导入 CPA</button><button className="button button-secondary" disabled={busy} onClick={onRefresh} type="button"><RefreshCw size={14} /> 刷新</button></div></div>
      {importResult ? <div className={importResult.failed_files ? 'import-summary has-errors' : 'import-summary'}><FileUp size={16} /><div><strong>已导入 {importResult.imported_accounts} 个账号</strong><span>{importResult.successful_files} 个文件成功{importResult.failed_files ? `，${importResult.failed_files} 个文件失败` : ''}</span>{importResult.failures?.length ? <ul>{importResult.failures.slice(0, 5).map((failure, index) => <li key={`${failure.file}-${index}`}><b>{failure.file}</b><span>{failure.error}</span></li>)}</ul> : null}</div><button className="icon-button" onClick={() => setImportResult(null)} title="关闭导入结果" type="button"><X size={14} /></button></div> : null}
      {!accounts.length ? <div className="empty-state table-shell"><Users size={24} /><strong>暂无可用账号</strong><span>完成一次注册后，凭据将自动加密导入。</span></div> : (
        <div className="gateway-table accounts-table">
          <div className="gateway-table-head"><span>账号</span><span>状态</span><span>并发</span><span>操作</span></div>
          {accounts.map((account) => <AccountRow account={account} key={account.id} onDelete={onDelete} onUpdate={onUpdate} />)}
        </div>
      )}
    </section>
  );
}

function AccountRow({ account, onDelete, onUpdate }) {
	const [draft, setDraft] = useState({ enabled: account.enabled, max_concurrent: account.max_concurrent });
	useEffect(() => setDraft({ enabled: account.enabled, max_concurrent: account.max_concurrent }), [account.enabled, account.max_concurrent]);
	const save = () => onUpdate({ id: account.id, name: account.name || '', ...draft });
  return <div className="gateway-table-row"><div><strong>{account.name || account.email || `账号 ${account.id}`}</strong><small>{account.email || account.user_id || account.provider}</small></div><label className="compact-toggle"><input checked={draft.enabled} onChange={(e) => setDraft((value) => ({ ...value, enabled: e.target.checked }))} type="checkbox" /><span>{draft.enabled ? '已启用' : '已停用'}</span></label><input className="concurrency-input" max="64" min="1" onChange={(e) => setDraft((value) => ({ ...value, max_concurrent: Number(e.target.value) || 1 }))} type="number" value={draft.max_concurrent} /><div className="row-actions"><button className="icon-button bordered" onClick={save} title="保存账号" type="button"><Save size={14} /></button><button className="icon-button bordered danger-icon" onClick={() => onDelete(account.id)} title="删除账号" type="button"><Trash2 size={14} /></button></div></div>;
}
