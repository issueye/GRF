export function StatusBar({ dashboard, bootstrapInfo }) {
  return (
    <footer className="statusbar">
      <span><i className={dashboard.running ? 'dot-success' : 'dot-muted'} />后台: {dashboard.running ? '运行中' : '空闲'}</span>
      <span>PID: {dashboard.pid || '—'}</span>
      <span>Worker: {dashboard.workers?.s || 0}</span>
      <span className="statusbar-path" title={bootstrapInfo?.data_root}>{bootstrapInfo?.data_root || '正在读取数据目录'}</span>
      <span>v{bootstrapInfo?.version || '—'}</span>
    </footer>
  );
}
