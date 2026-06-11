import { useEffect } from 'react';
import App from './App';
import { useCustomConfig } from './hooks/useCustomConfig';

export function AppWrapper() {
  const { config, isLoading } = useCustomConfig();

  useEffect(() => {
    // Dynamically set page title
    document.title = config.pageTitle;

    // Dynamically set favicon (only when a path is configured; setting an empty
    // href would make the browser re-request the page itself as the icon).
    const favicon = document.querySelector('link[rel="icon"]');
    if (favicon && config.faviconPath) {
      favicon.setAttribute('href', config.faviconPath);
    }
  }, [config]);

  // Show loading state while config is being fetched
  if (isLoading) {
    return (
      <div style={{
        display: 'flex',
        justifyContent: 'center',
        alignItems: 'center',
        height: '100vh',
        backgroundColor: '#ffffff',
        color: '#000000'
      }}>
        <div>Loading...</div>
      </div>
    );
  }

  return <App config={config} />;
}
