import { CirclePause, CirclePlay, RefreshCw, Search, Trash2 } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';

export function GatewayLogsPage({ busy, logs, onClear, onRefresh }) {
  const [paused, setPaused] = useState(false);
	const [frozenLogs, setFrozenLogs] = useState([]);
  const [filter, setFilter] = useState('all');
  const [query, setQuery] = useState('');
  const [autoScroll, setAutoScroll] = useState(true);
  const terminalRef = useRef(null);

	const sourceLogs = paused ? frozenLogs : logs;
  const visibleLogs = useMemo(() => sourceLogs.filter((entry) => {
    const statusMatch = filter === 'all' || (filter === 'success' && entry.status < 400) || (filter === 'error' && entry.status >= 400);
    const needle = query.trim().toLowerCase();
    const textMatch = !needle || [entry.method, entry.path, entry.model, entry.account_id, entry.user_agent, entry.status].join(' ').toLowerCase().includes(needle);
    return statusMatch && textMatch;
	}).reverse(), [filter, query, sourceLogs]);
	const togglePaused = () => { if (!paused) setFrozenLogs(logs); setPaused((value) => !value); };

  useEffect(() => {
    if (!paused && autoScroll && terminalRef.current) terminalRef.current.scrollTop = terminalRef.current.scrollHeight;
  }, [autoScroll, paused, visibleLogs]);

  return (
    <section className="page gateway-logs-page" aria-labelledby="gateway-logs-title">
      <div className="page-heading">
        <div><h1 id="gateway-logs-title">网关日志</h1><p>当前进程中的实时请求记录。</p></div>
        <div className="heading-actions"><button className="button button-secondary" disabled={busy} onClick={onRefresh} type="button"><RefreshCw size={14} /> 刷新</button><button className="button button-secondary danger-label" disabled={busy || !logs.length} onClick={onClear} type="button"><Trash2 size={14} /> 清空</button></div>
      </div>

      <div className="log-toolbar">
		<button className={paused ? 'button button-primary' : 'button button-secondary'} onClick={togglePaused} type="button">{paused ? <CirclePlay size={14} /> : <CirclePause size={14} />}{paused ? '继续' : '暂停'}</button>
        <div className="segmented-control" aria-label="状态筛选">{[['all', '全部'], ['success', '成功'], ['error', '错误']].map(([value, label]) => <button className={filter === value ? 'active' : ''} key={value} onClick={() => setFilter(value)} type="button">{label}</button>)}</div>
        <label className="log-search"><Search size={14} /><input onChange={(event) => setQuery(event.target.value)} placeholder="路径、模型、UA" value={query} /></label>
        <label className="compact-toggle log-autoscroll"><input checked={autoScroll} onChange={(event) => setAutoScroll(event.target.checked)} type="checkbox" /><span>自动滚动</span></label>
		<span className="log-count">{visibleLogs.length} / {sourceLogs.length}</span>
      </div>

      <div className="gateway-terminal" ref={terminalRef}>
        <div className="gateway-terminal-head"><span>TIME</span><span>METHOD</span><span>STATUS</span><span>LATENCY</span><span>REQUEST</span></div>
        {!visibleLogs.length ? <div className="gateway-terminal-empty"><i /><span>{paused ? '监控已暂停' : '等待网关请求'}</span></div> : visibleLogs.map((entry) => <LogLine entry={entry} key={entry.id} />)}
      </div>
    </section>
  );
}

function LogLine({ entry }) {
  const level = entry.status >= 500 ? 'server-error' : entry.status >= 400 ? 'client-error' : 'success';
  const time = new Date(entry.timestamp).toLocaleTimeString([], { hour12: false });
  return <div className={`gateway-log-line ${level}`}><time>{time}</time><strong>{entry.method}</strong><span className="log-status">{entry.status}</span><span>{entry.duration_ms} ms</span><div><code>{entry.path}</code>{entry.model ? <span>model={entry.model}</span> : null}{entry.account_id ? <span>account={entry.account_id}</span> : null}{entry.user_agent ? <small title={entry.user_agent}>ua={entry.user_agent}</small> : null}</div></div>;
}
