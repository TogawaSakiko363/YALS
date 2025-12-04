import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import App from './App.tsx';
import './index.css';
import { config } from './custom';

// Dynamically set page title
document.title = config.pageTitle;

// Dynamically set favicon
const favicon = document.querySelector('link[rel="icon"]');
if (favicon) {
  favicon.setAttribute('href', new URL(config.faviconPath, import.meta.url).href);
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>
);
