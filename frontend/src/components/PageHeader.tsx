import { Settings } from 'lucide-react';
import { CustomConfig } from '../hooks/useCustomConfig';

interface PageHeaderProps {
  config: CustomConfig;
  active: 'home' | 'status' | 'probes';
}

// Shared top navigation for the public pages (looking glass / status / probes).
export function PageHeader({ config, active }: PageHeaderProps) {
  return (
    <header className="app-header">
      <div className="container max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 w-full">
        <div className="header-content">
          <div className="header-left">
            <a href="/" className="logo-container" aria-label="Looking Glass">
              <img src={config.logoPath} alt="Logo" className="logo-image" />
            </a>
            <a href="/" className="app-title" aria-label="Looking Glass home">
              <h1 className="title-large">Looking Glass</h1>
            </a>
            <nav className="page-nav">
              <a href="/status" className={`page-nav-link ${active === 'status' ? 'active' : ''}`}>Status</a>
              <a href="/probes" className={`page-nav-link ${active === 'probes' ? 'active' : ''}`}>Probes</a>
            </nav>
          </div>
          <div className="header-right">
            <a href="/control" className="header-gear" title="Control panel" aria-label="Control panel">
              <Settings className="w-5 h-5" />
            </a>
          </div>
        </div>
      </div>
    </header>
  );
}
