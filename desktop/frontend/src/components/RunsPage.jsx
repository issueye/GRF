import { ExternalLink, FolderOpen } from 'lucide-react';

export function RunsPage({ runs, onOpenPath }) {
  return (
    <section className="page runs-page" aria-labelledby="runs-title">
      <div className="page-heading"><div><span className="eyebrow">ARTIFACTS</span><h1 id="runs-title">运行产物</h1><p>按运行批次查看 SSO 与 CPA 文件，文件始终保存在本地。</p></div></div>
      <div className="table-shell">
        <div className="table-header"><span>运行批次</span><span>CPA</span><span>SSO</span><span>更新时间</span><span aria-hidden="true" /></div>
        {runs.length === 0 ? (
          <div className="empty-state"><FolderOpen size={22} /><strong>暂无运行产物</strong><span>成功完成任务后，批次会出现在这里。</span></div>
        ) : runs.map((run) => (
          <div className="table-row" key={run.id}>
            <div><strong>{run.id}</strong><small title={run.path}>{run.path}</small></div>
            <span className="count-badge success">{run.cpa_count}</span>
            <span className="count-badge">{run.sso_count}</span>
            <time>{new Date(run.updated_at).toLocaleString('zh-CN', { hour12: false })}</time>
            <button className="icon-button" onClick={() => onOpenPath(run.path)} title="在文件资源管理器中打开" type="button"><ExternalLink size={15} /></button>
          </div>
        ))}
      </div>
    </section>
  );
}
