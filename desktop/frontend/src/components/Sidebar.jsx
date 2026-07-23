import { Activity, Box, Boxes, KeyRound, LayoutDashboard, Network, ScrollText, Settings2, Users } from 'lucide-react';

const groups = [
  { label: '注册', items: [
    { id: 'overview', label: '总览', icon: LayoutDashboard },
    { id: 'logs', label: '实时日志', icon: Activity },
    { id: 'runs', label: '产物', icon: Boxes },
  ] },
  { label: 'API 网关', items: [
    { id: 'gateway', label: '网关服务', icon: Network },
    { id: 'gateway-logs', label: '请求日志', icon: ScrollText },
    { id: 'accounts', label: '账号', icon: Users },
    { id: 'models', label: '模型', icon: Box },
    { id: 'keys', label: 'API Keys', icon: KeyRound },
  ] },
  { label: '系统', items: [
    { id: 'settings', label: '设置', icon: Settings2 },
  ] },
];

export function Sidebar({ active, onChange, bootstrapInfo }) {
  return (
    <aside className="sidebar" aria-label="主导航">
      <nav>
        {groups.map((group) => (
          <div className="nav-group" key={group.label}>
            <div className="nav-group-label">{group.label}</div>
            {group.items.map(({ id, label, icon: Icon }) => (
              <button
                aria-current={active === id ? 'page' : undefined}
                className={active === id ? 'nav-item active' : 'nav-item'}
                key={id}
                onClick={() => onChange(id)}
                title={label}
                type="button"
              >
                <Icon size={17} />
                <span><strong>{label}</strong></span>
              </button>
            ))}
          </div>
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
