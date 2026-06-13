import { CustomConfig } from './hooks/useCustomConfig';
import { ControlPanel } from './pages/ControlPanel';
import { LookingGlass } from './pages/LookingGlass';
import { ProbesPage } from './pages/ProbesPage';
import { StatusPage } from './pages/StatusPage';

interface AppProps {
  config: CustomConfig;
}

// App is a thin pathname router. Each page is self-contained and owns its own
// useYalsClient, so exactly one client instance is live per route. The `.html`
// aliases are kept so older bookmarks keep working.
function App({ config }: AppProps) {
  const pathname = window.location.pathname;

  if (pathname === '/control' || pathname === '/control.html') {
    return <ControlPanel />;
  }
  if (pathname === '/status' || pathname === '/status.html') {
    return <StatusPage config={config} />;
  }
  if (pathname === '/probes' || pathname === '/probes.html') {
    return <ProbesPage config={config} />;
  }
  return <LookingGlass config={config} />;
}

export default App;
