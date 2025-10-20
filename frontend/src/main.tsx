import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import App from './App.tsx';
import './index.css';
import { config } from './custom';

// 动态设置网页标题
document.title = config.pageTitle;

// 动态设置favicon
const favicon = document.querySelector('link[rel="icon"]');
if (favicon) {
  favicon.setAttribute('href', new URL(config.faviconPath, import.meta.url).href);
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>
);
