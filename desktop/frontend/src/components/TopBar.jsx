import { Minus, RefreshCw, Square, X } from 'lucide-react';
import { windowActions } from '../lib/native.js';

export function TopBar({ running, busy, onRefresh }) {
  return (
    <header className="topbar">
      <div className="brand" aria-label="GRF">
        <span className={`brand-mark ${running ? 'is-running' : ''}`} aria-hidden="true">G</span>
        <strong>GRF</strong>
        <span>Registration Console</span>
      </div>
      <div className="topbar-drag" aria-hidden="true" />
      <div className="topbar-actions" data-no-drag>
        <span className={`status-pill ${running ? 'is-online' : 'is-idle'}`} aria-live="polite">
          <i /> {running ? '任务运行中' : '系统就绪'}
        </span>
        <button className="icon-button" disabled={busy} onClick={onRefresh} title="刷新状态" type="button">
          <RefreshCw className={busy ? 'spin' : ''} size={15} />
        </button>
        <div className="window-controls" role="group" aria-label="窗口控制">
          <button onClick={windowActions.minimise} title="最小化" type="button"><Minus size={14} /></button>
          <button onClick={windowActions.toggleMaximise} title="最大化或还原" type="button"><Square size={11} /></button>
          <button className="window-close" onClick={windowActions.close} title="关闭" type="button"><X size={15} /></button>
        </div>
      </div>
    </header>
  );
}
