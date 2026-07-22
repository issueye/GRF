import { Copy, RefreshCw } from 'lucide-react';

export function LogsPage({ log, path, busy, onRefresh, onOpenPath }) {
  async function copyLog() {
    await navigator.clipboard?.writeText(log || '');
  }
  return (
    <section className="page logs-page" aria-labelledby="logs-title">
      <div className="page-heading">
        <div><span className="eyebrow">LIVE STREAM</span><h1 id="logs-title">实时日志</h1><p>用于观察请求阶段、浏览器池和 OAuth 执行状态。</p></div>
        <div className="heading-actions">
          <button className="button button-secondary" disabled={!path} onClick={() => onOpenPath(path)} type="button">打开文件</button>
          <button className="icon-button bordered" onClick={copyLog} title="复制日志" type="button"><Copy size={15} /></button>
          <button className="icon-button bordered" disabled={busy} onClick={onRefresh} title="刷新日志" type="button"><RefreshCw className={busy ? 'spin' : ''} size={15} /></button>
        </div>
      </div>
      <div className="terminal-shell">
        <div className="terminal-header"><span><i /> gtr worker</span><small>{path || '尚未生成日志文件'}</small></div>
        <pre aria-live="polite">{log || '等待日志输出…'}</pre>
      </div>
    </section>
  );
}
