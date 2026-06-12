import { useEffect, useState } from 'react';
import { Github } from 'lucide-react';
import { CustomConfig } from '../hooks/useCustomConfig';

interface PageFooterProps {
  config: CustomConfig;
}

// Shared footer for the public pages (looking glass / status / probes). It pulls
// the version from the public /api/version endpoint itself so every page renders
// an identical footer without threading the value through each page.
export function PageFooter({ config }: PageFooterProps) {
  const [version, setVersion] = useState('unknown');

  useEffect(() => {
    let cancelled = false;
    fetch('/api/version', { headers: { Accept: 'application/json' } })
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error(String(r.status)))))
      .then((d: { version?: string }) => {
        if (!cancelled && d.version) setVersion(d.version);
      })
      .catch(() => {
        // Leave the placeholder; a missing version is not worth surfacing.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <footer className="app-footer">
      <div className="container max-w-7xl mx-auto px-3 sm:px-4 lg:px-6 py-3 sm:py-4 w-full">
        <div className="footer-content">
          <div className="footer-left">
            <a href="https://github.com/TogawaSakiko363/YALS" target="_blank" rel="noopener noreferrer" className="github-link flex items-center gap-0.5">
              Powered by YALS
              <Github className="w-4 h-4" />
            </a>
            <p className="version-info">Version {version}</p>
          </div>
          <div className="footer-right">
            <p>{config.footerRightText}</p>
          </div>
        </div>
      </div>
    </footer>
  );
}
