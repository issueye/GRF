import { Box } from 'lucide-react';

export function ModelsPage({ models }) {
  return <section className="page" aria-labelledby="models-title">
    <div className="page-heading">
      <h1 id="models-title">模型</h1>
    </div>
    <div className="model-list">
      {models.map((model) => 
        <div className="model-row" key={model.id}>
          <span className="model-icon">
            <Box size={17} />
          </span>
        <div>
          <strong>{model.id}</strong>
          <small>{model.owned_by} · {model.object}</small>
        </div>
        <code>{model.id}</code>
        </div>)
      }
    </div>
  </section>;
}
