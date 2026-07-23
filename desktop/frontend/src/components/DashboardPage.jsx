import { ArrowRight, CircleCheck, FolderOpen, Gauge, Octagon, Play, Square } from 'lucide-react';

const stages = [
  { id: 'clearance', label: '网络预热' },
  { id: 'turnstile', label: 'Turnstile' },
  { id: 'email', label: '邮箱验证' },
  { id: 'register', label: '账号注册' },
  { id: 'oauth', label: 'OAuth' },
  { id: 'probe', label: 'CPA 就绪' },
];

function phaseIndex(dashboard) {
  if (!dashboard.running && dashboard.done > 0) return stages.length;
  if (dashboard.phase === 'clearance') return 0;
  if (dashboard.phase === 'register') {
    // Email and Turnstile workers run concurrently. Until a paired account is
    // actually submitted, the usual bottleneck is the Turnstile token queue.
    return dashboard.phase_detail?.startsWith('正在注册 ') ? 3 : 1;
  }
  if (dashboard.phase === 'oauth') return 4;
  if (dashboard.phase === 'probe') return 5;
  return -1;
}

function Metric({ label, value, note, tone = 'neutral' }) {
  return (
    <div className={`metric metric-${tone}`}>
      <span>{label}</span>
      <strong>{value}</strong>
      <small>{note}</small>
    </div>
  );
}

export function DashboardPage({ dashboard, target, threads, setTarget, setThreads, busy, onStart, onStop, onOpenOutput, log }) {
  const progress = dashboard.target > 0 ? Math.min(100, Math.round((dashboard.done / dashboard.target) * 100)) : 0;
  const current = phaseIndex(dashboard);
  return (
    <section className="page dashboard-page" aria-labelledby="overview-title">
      <div className="page-heading">
        <div>
          <h1 id="overview-title">注册控制台</h1>
        </div>
        {dashboard.output_dir ? (
          <button className="button button-secondary" onClick={() => onOpenOutput(dashboard.output_dir)} type="button">
            <FolderOpen size={15} /> 打开当前产物
          </button>
        ) : null}
      </div>

      <div className="launch-panel">
        <div className="launch-copy">
          <span className={`large-state ${dashboard.running ? 'running' : ''}`}>
            {dashboard.running ? <Gauge size={18} /> : <CircleCheck size={18} />}
            {dashboard.running ? dashboard.phase_detail || '任务运行中' : '可以开始新的任务'}
          </span>
          <small>{dashboard.running ? `Run ${dashboard.run_id || '—'} · PID ${dashboard.pid}` : '参数只影响下一次启动，不会写入长期配置。'}</small>
        </div>
        <label className="compact-field">
          <span>目标数量</span>
          <input disabled={dashboard.running || busy} max="10000" min="1" onChange={(event) => setTarget(Number(event.target.value))} type="number" value={target} />
        </label>
        <label className="compact-field">
          <span>并发线程</span>
          <input disabled={dashboard.running || busy} max="8" min="1" onChange={(event) => setThreads(Number(event.target.value))} type="number" value={threads} />
        </label>
        {dashboard.running ? (
          <button className="button button-danger" disabled={busy} onClick={onStop} type="button"><Square size={14} /> 停止任务</button>
        ) : (
          <button className="button button-primary" disabled={busy || target < 1 || threads < 1} onClick={onStart} type="button"><Play size={15} /> 开始运行</button>
        )}
      </div>

      <div className="metrics-grid" aria-label="运行指标">
        <Metric label="完成进度" value={`${dashboard.done || 0} / ${dashboard.target || target}`} note={`${progress}% 已完成`} tone="brand" />
        <Metric label="SSO" value={dashboard.sso_count || 0} note="已获取会话" />
        <Metric label="OAuth" value={dashboard.oauth_count || 0} note="授权成功" tone="success" />
        <Metric label="失败" value={dashboard.fail_count || 0} note="本次运行累计" tone={dashboard.fail_count ? 'danger' : 'neutral'} />
      </div>

      <section className="section-block pipeline-block">
        <div className="section-title"><div><h2>执行流水线</h2><p>当前阶段：{dashboard.phase_detail || '等待启动'}</p></div><span>{dashboard.rate_per_min ? `${dashboard.rate_per_min.toFixed(1)} / 分钟` : '实时状态'}</span></div>
        <div className="pipeline" role="list">
          {stages.map((stage, index) => {
            const complete = current > index;
            const active = current === index;
            return (
              <div className={`pipeline-stage ${complete ? 'complete' : ''} ${active ? 'active' : ''}`} key={stage.id} role="listitem">
                <span>{complete ? <CircleCheck size={15} /> : active ? <span className="pulse-dot" /> : index + 1}</span>
                <strong>{stage.label}</strong>
                {index < stages.length - 1 ? <ArrowRight className="pipeline-arrow" size={14} /> : null}
              </div>
            );
          })}
        </div>
      </section>

      <section className="section-block log-preview">
        <div className="section-title"><div><h2>最近活动</h2><p>最新日志会在任务运行期间自动刷新。</p></div>{dashboard.error ? <span className="danger-label"><Octagon size={13} /> 需要处理</span> : null}</div>
        {dashboard.error ? <div className="inline-error" role="alert">{dashboard.error}</div> : null}
        <pre>{log || '暂无运行日志。点击“开始运行”后，关键事件会显示在这里。'}</pre>
      </section>
    </section>
  );
}
