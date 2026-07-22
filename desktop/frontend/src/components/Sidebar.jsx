import { Activity, Boxes, LayoutDashboard, Settings2 } from 'lucide-react';

const items = [
  { id: 'overview', label: '总览', description: '任务与进度', icon: LayoutDashboard },
  { id: 'logs', label: '实时日志', description: '运行事件流', icon: Activity },
  { id: 'runs', label: '产物', description: '历史运行结果', icon: Boxes },
  { id: 'settings', label: '设置', description: '运行环境配置', icon: Settings2 },
];

export function Sidebar({ active, onChange, bootstrapInfo }) {
  return (
    <aside className="sidebar" aria-label="主导航">
      <nav>
        {items.map(({ id, label, description, icon: Icon }) => (
          <button
            aria-current={active === id ? 'page' : undefined}
            className={active === id ? 'nav-item active' : 'nav-item'}
            key={id}
            onClick={() => onChange(id)}
            title={label}
            type="button"
          >
            <Icon size={17} />
            <span><strong>{label}</strong><small>{description}</small></span>
          </button>
        ))}
      </nav>
      <div className="environment-summary">
        <span>运行环境</span>
        <strong>{bootstrapInfo?.platform === 'windows' ? 'Windows Native' : bootstrapInfo?.platform || '检测中'}</strong>
        <small title={bootstrapInfo?.chrome_path || ''}>{bootstrapInfo?.chrome_path ? 'Chrome 已就绪' : '未检测到 Chrome'}</small>
      </div>
    </aside>
  );
}
