import { useEffect, useState } from 'react';
import { Github } from 'lucide-react';
import { CustomConfig } from '../hooks/useCustomConfig';

interface PageFooterProps {
  config: CustomConfig;
}

// Shared footer for the public pages (looking glass / status / probes). It pulls
// the version from the public /api/version endpoint itself so every page renders
// an identical footer without threading the value through each page. Because each
// page is a full navigation, the last-known version is cached in localStorage and
// used as the initial value — so the footer shows it immediately instead of
// flashing a placeholder while the fetch is in flight.
const VERSION_CACHE_KEY = 'yals_version';

const readCachedVersion = (): string => {
  try {
    return localStorage.getItem(VERSION_CACHE_KEY) || '';
  } catch {
    return '';
  }
};

export function PageFooter({ config }: PageFooterProps) {
  const [version, setVersion] = useState(readCachedVersion);

  useEffect(() => {
    let cancelled = false;
    fetch('/api/version', { headers: { Accept: 'application/json' } })
      .then((r) => (r.ok ? r.json() : Promise.reject(new Error(String(r.status)))))
      .then((d: { version?: string }) => {
        if (!cancelled && d.version) {
          setVersion(d.version);
          try {
            localStorage.setItem(VERSION_CACHE_KEY, d.version);
          } catch {
            // localStorage may be unavailable (private mode); the in-memory
            // value still renders for this page load.
          }
        }
      })
      .catch(() => {
        // Leave the cached/empty value; a missing version is not worth surfacing.
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
            <p className="version-info">Version {version || '—'}</p>
          </div>
          <div className="footer-right">
            <p>{config.footerRightText}</p>
          </div>
        </div>
      </div>
    </footer>
  );
}
