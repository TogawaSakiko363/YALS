import { useState, useEffect } from 'react';

export interface CustomConfig {
  pageTitle: string;
  footerRightText: string;
  faviconPath: string;
  logoPath: string;
  backgroundColor: string;
}

const defaultConfig: CustomConfig = {
  pageTitle: 'Yet Another Looking Glass',
  footerRightText: '© 2026 TogawaSakiko363',
  faviconPath: '',
  logoPath: '',
  backgroundColor: '#ffffff'
};

export const useCustomConfig = () => {
  const [config, setConfig] = useState<CustomConfig>(defaultConfig);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    const loadConfig = async () => {
      try {
        const basePath = window.location.pathname.startsWith('/control.html') ? '/custom/config.json' : '/custom/config.json';
        const response = await fetch(basePath);

        if (!response.ok) {
          throw new Error(`Failed to load custom config: ${response.status}`);
        }

        const data = await response.json();
        const mergedConfig: CustomConfig = {
          ...defaultConfig,
          ...data
        };

        setConfig(mergedConfig);
        setError(null);
      } catch (err) {
        console.warn('Failed to load custom config, using defaults:', err);
        setError(err instanceof Error ? err : new Error('Unknown error'));
      } finally {
        setIsLoading(false);
      }
    };

    loadConfig();
  }, []);

  return { config, isLoading, error };
};
