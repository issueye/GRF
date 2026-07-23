import { FileText, Save } from 'lucide-react';

export function SettingsPage({ settings, setSettings, configPath, busy, onSave, onOpenConfig }) {
  if (!settings) {
    return <section className="page"><div className="loading-state">正在读取配置…</div></section>;
  }
  const patch = (key, value) => setSettings((current) => ({ ...current, [key]: value }));
  return (
    <section className="page settings-page" aria-labelledby="settings-title">
      <div className="page-heading">
        <h1 id="settings-title">运行设置</h1>
        <div className="heading-actions">
          <button className="button button-secondary" onClick={onOpenConfig} type="button"><FileText size={14} /> 打开配置文件</button>
          <button className="button button-primary" disabled={busy} onClick={onSave} type="button"><Save size={14} /> 保存设置</button>
        </div>
      </div>
      <div className="config-path"><span>配置文件</span><code>{configPath}</code></div>
      <div className="settings-grid">
        <section className="settings-section">
          <div><h2>注册与验证</h2><p>浏览器、出口代理和 Cloudflare 清障服务。</p></div>
          <label><span>Turnstile Provider</span><select onChange={(e) => patch('turnstile_provider', e.target.value)} value={settings.turnstile_provider}><option value="browser">browser（系统默认）</option><option value="go-browser">go-browser</option><option value="playwright">playwright</option><option value="lite">lite solver</option><option value="chromedp">chromedp</option></select></label>
          <label><span>全局出口代理</span><input onChange={(e) => patch('register_proxy', e.target.value)} placeholder="http://127.0.0.1:7897" value={settings.register_proxy} /><small>同时用于注册、邮箱和 OAuth 请求；留空则直连。</small></label>
          <label><span>FlareSolverr URL</span><input onChange={(e) => patch('flaresolverr_url', e.target.value)} value={settings.flaresolverr_url} /></label>
          <label className="switch-row"><span><strong>启用 Clearance</strong><small>启动前预热目标站点会话</small></span><input checked={settings.clearance_enabled} onChange={(e) => patch('clearance_enabled', e.target.checked)} type="checkbox" /></label>
        </section>
        <section className="settings-section">
          <div><h2>邮箱服务</h2><p>验证码接收方式与自建服务地址。</p></div>
          <label><span>邮箱模式</span><select onChange={(e) => patch('email_mode', e.target.value)} value={settings.email_mode}><option value="tempmail">tempmail</option><option value="testmail">testmail.app</option><option value="custom">自建域名</option></select></label>
          <label><span>邮箱 API</span><input onChange={(e) => patch('email_api', e.target.value)} value={settings.email_api} /></label>
          <label><span>邮箱域名</span><input onChange={(e) => patch('email_domain', e.target.value)} placeholder="仅自建域名模式需要" value={settings.email_domain} /></label>
        </section>
        <section className="settings-section wide">
          <div><h2>CPA Management</h2><p>可选的结果上传目标；访问密钥仍在 config.env 中设置。</p></div>
          <label><span>Management Base URL</span><input onChange={(e) => patch('cpa_management_base', e.target.value)} value={settings.cpa_management_base} /></label>
          <label className="switch-row"><span><strong>成功后自动上传</strong><small>本地文件始终会保留</small></span><input checked={settings.cpa_upload_enabled} onChange={(e) => patch('cpa_upload_enabled', e.target.checked)} type="checkbox" /></label>
        </section>
        <section className="settings-section wide gateway-settings">
          <div><h2>内嵌 API 网关</h2><p>提供 OpenAI / Anthropic 兼容接口。默认关闭，仅在手动开启后监听。</p></div>
          <div className="gateway-setting-fields">
            <label className="switch-row"><span><strong>启用 API 网关</strong><small>{settings.api_enabled ? `http://${settings.api_listen_host}:${settings.api_listen_port}` : '当前不监听任何端口'}</small></span><input checked={settings.api_enabled} onChange={(e) => patch('api_enabled', e.target.checked)} type="checkbox" /></label>
            {settings.api_enabled && !['127.0.0.1', '::1', 'localhost'].includes(settings.api_listen_host) ? <div className="listen-warning">当前地址可能被局域网或公网访问，请确保 API Key 已妥善保管。</div> : null}
            <div className="listen-fields"><label><span>监听地址</span><input onChange={(e) => patch('api_listen_host', e.target.value)} value={settings.api_listen_host} /></label><label><span>端口</span><input max="65535" min="1" onChange={(e) => patch('api_listen_port', Number(e.target.value))} type="number" value={settings.api_listen_port} /></label></div>
          </div>
        </section>
      </div>
    </section>
  );
}
