// Web customization config file
export const config = {
  // Page title
  pageTitle: 'Example Networks - Looking Glass',
  
  // Footer right text content
  footerRightText: '© 2025 Example Networks, LLC.',
  
  // Web icon path, please put image files in public/images directory
  faviconPath: '/images/favicon.png',
  
  // Web top-left logo path, please put image files in public/images directory
  logoPath: '/images/logo.png',
  
  // Web background color
  backgroundColor: '#f5f4f1'
};

// Export type definition for TypeScript type checking
export type ConfigType = typeof config;