import { StrictMode, useEffect } from 'react';
import { createRoot } from 'react-dom/client';
import App from './App.tsx';
import './index.css';
import { useCustomConfig } from './hooks/useCustomConfig.ts';

function AppWrapper() {
  const { config, isLoading } = useCustomConfig();

  useEffect(() => {
    // Dynamically set page title
    document.title = config.pageTitle;

    // Dynamically set favicon
    const favicon = document.querySelector('link[rel="icon"]');
    if (favicon) {
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
        backgroundColor: '#f5f5f5'
      }}>
        <div>Loading...</div>
      </div>
    );
  }

  return <App config={config} />;
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AppWrapper />
  </StrictMode>
);
