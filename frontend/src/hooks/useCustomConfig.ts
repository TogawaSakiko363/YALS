import { useState, useEffect } from 'react';

export interface CustomConfig {
  pageTitle: string;
  footerRightText: string;
  faviconPath: string;
  logoPath: string;
  backgroundColor: string;
}

// Default configuration as fallback
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
        const response = await fetch('/custom/config.json');
        
        if (!response.ok) {
          throw new Error(`Failed to load custom config: ${response.status}`);
        }

        const data = await response.json();
        
        // Merge with default config to ensure all fields exist
        const mergedConfig: CustomConfig = {
          ...defaultConfig,
          ...data
        };
        
        setConfig(mergedConfig);
        setError(null);
      } catch (err) {
        console.warn('Failed to load custom config, using defaults:', err);
        setError(err instanceof Error ? err : new Error('Unknown error'));
        // Keep using default config on error
      } finally {
        setIsLoading(false);
      }
    };

    loadConfig();
  }, []);

  return { config, isLoading, error };
};
