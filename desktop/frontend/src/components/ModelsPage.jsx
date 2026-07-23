import { Box } from 'lucide-react';

export function ModelsPage({ models }) {
  return <section className="page" aria-labelledby="models-title"><div className="page-heading"><div><h1 id="models-title">模型</h1><p>当前网关公开的模型标识，可直接用于兼容 API 请求。</p></div></div><div className="model-list">{models.map((model) => <div className="model-row" key={model.id}><span className="model-icon"><Box size={17} /></span><div><strong>{model.id}</strong><small>{model.owned_by} · {model.object}</small></div><code>{model.id}</code></div>)}</div></section>;
}
