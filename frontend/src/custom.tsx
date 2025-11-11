// Web customization config file
export const config = {
  // Page title
  pageTitle: 'Example Networks - Looking Glass',
  
  // Footer right text content
  footerRightText: '© 2025 Example Networks, LLC.',
  
  // Web icon path, please put image files in public/images directory
  faviconPath: '/images/favicon.ico',
  
  // Web top-left logo path, please put image files in public/images directory
  logoPath: '/images/Example.svg',
  
  // Web background color
  backgroundColor: '#ffffff'
};

// Export type definition for TypeScript type checking
export type ConfigType = typeof config;