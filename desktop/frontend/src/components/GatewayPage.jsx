import { CircleStop, Power, Save, Server, ShieldCheck, Sparkles } from 'lucide-react';

const endpoints = [
  'GET /v1/models',
  'POST /v1/responses',
  'POST /v1/responses/compact',
  'POST /v1/chat/completions',
  'POST /v1/messages',
];

function formatCount(value) {
  const n = Number(value) || 0;
  return n.toLocaleString('en-US');
}

export function GatewayPage({ busy, settings, setSettings, status, onSave }) {
  if (!settings) {
    return <section className="page"><div className="loading-state">正在读取网关配置...</div></section>;
  }

  const patch = (key, value) => setSettings((current) => ({ ...current, [key]: value }));
  const running = Boolean(status?.running);
  const publicHost = !['127.0.0.1', '::1', 'localhost'].includes(settings.api_listen_host);
  const configuredAddress = `${settings.api_listen_host}:${settings.api_listen_port}`;
  const usage = status?.token_usage || {};
  const inputTokens = Number(usage.input_tokens) || 0;
  const outputTokens = Number(usage.output_tokens) || 0;
  const totalTokens = Number(usage.total_tokens) || (inputTokens + outputTokens);
  const requestCount = Number(usage.request_count ?? status?.request_count) || 0;

  return (
    <section className="page gateway-page" aria-labelledby="gateway-title">
      <div className="page-heading">
        <h1 id="gateway-title">API 网关</h1>
        <button className="button button-primary" disabled={busy} onClick={() => onSave()} type="button"><Save size={14} /> 保存并应用</button>
      </div>

      <div className={running ? 'gateway-hero is-running' : 'gateway-hero'}>
        <span className="gateway-hero-icon">{running ? <Power size={20} /> : <CircleStop size={20} />}</span>
        <div><span>服务状态</span><strong>{running ? '正在运行' : '已关闭'}</strong><small>{running ? status.address : configuredAddress}</small></div>
        <label className="switch-row gateway-power"><span><strong>{settings.api_enabled ? '已设为启用' : '已设为关闭'}</strong><small>保存后立即生效</small></span><input checked={settings.api_enabled} onChange={(event) => patch('api_enabled', event.target.checked)} type="checkbox" /></label>
      </div>

      {status?.error ? <div className="inline-error gateway-error">{status.error}</div> : null}

      <section className="gateway-token-summary" aria-label="Token 汇总">
        <div className="gateway-token-summary-head">
          <span className="gateway-token-summary-icon"><Sparkles size={16} /></span>
          <div>
            <h2>Token 汇总</h2>
            <p>自进程启动累计；清空请求日志时重置。</p>
          </div>
        </div>
        <div className="gateway-token-grid">
          <div className="gateway-token-card is-total"><span>总 Token</span><strong>{formatCount(totalTokens)}</strong></div>
          <div className="gateway-token-card"><span>输入</span><strong>{formatCount(inputTokens)}</strong></div>
          <div className="gateway-token-card"><span>输出</span><strong>{formatCount(outputTokens)}</strong></div>
          <div className="gateway-token-card"><span>请求数</span><strong>{formatCount(requestCount)}</strong></div>
        </div>
      </section>

      <div>
        <div className="gateway-layout">
          <section className="gateway-panel">
            <div className="gateway-panel-title">
              <Server size={16} />
              <div>
                <h2>监听配置</h2>
                <p>服务绑定的网络接口与端口。</p>
                </div>
              </div>
            <div className="listen-fields">
              <label>
                <span>监听地址</span>
                <input onChange={(event) => patch('api_listen_host', event.target.value)} value={settings.api_listen_host} />
              </label>
              <label>
                <span>端口</span>
                <input max="65535" min="1" onChange={(event) => patch('api_listen_port', Number(event.target.value))} type="number" value={settings.api_listen_port} />
              </label>
            </div>
            <label>
              <span>上游出口代理</span>
              <input onChange={(event) => patch('register_proxy', event.target.value)} placeholder="留空则直连" value={settings.register_proxy} />
              <small>与注册任务共用此出口配置。</small>
            </label>
            <label className="switch-row gateway-stream-default">
              <span>
                <strong>默认流式输出</strong>
                <small>仅在客户端未传 stream 时生效</small>
              </span>
              <input checked={settings.api_stream_default} onChange={(event) => patch('api_stream_default', event.target.checked)} type="checkbox" />
            </label>
            {settings.api_enabled && publicHost ? <div className="listen-warning">当前地址可被其他设备访问，请仅向可信客户端分发 API Key。</div> : null}
          </section>

          <section className="gateway-panel">
            <div className="gateway-panel-title"><ShieldCheck size={16} /><div><h2>鉴权与协议</h2><p>所有推理端点均强制验证 API Key。</p></div></div>
            <div className="gateway-security"><span>密钥格式</span><code>grf_...</code><span>凭据存储</span><strong>AES-256-GCM</strong></div>
          </section>
        </div>

        <section className="gateway-endpoints">
          <div className="section-title">
            <div>
              <h2>可用端点</h2>
              <p>当前版本公开的兼容 API。</p>
            </div>
            <span>{endpoints.length} 个端点</span>
          </div>
          <div>{endpoints.map((endpoint) => { const [method, path] = endpoint.split(' '); return <div className="endpoint-row" key={endpoint}><span className={`method-tag method-${method.toLowerCase()}`}>{method}</span><code>{path}</code></div>; })}</div>
        </section>
      </div>
    </section>
  );
}
