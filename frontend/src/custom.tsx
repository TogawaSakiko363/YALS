// Web customization config file
export const config = {
  // Page title
  pageTitle: 'Exmaple Networks - Looking Glass',
  
  // Footer right text content
  footerRightText: '© 2025 Exmaple Networks, LLC.',
  
  // Web icon path, please put image files in public/images directory
  faviconPath: '/images/Exmaple.ico',
  
  // Web top-left logo path, please put image files in public/images directory
  logoPath: '/images/Exmaple.png',
  
  // Web background color
  backgroundColor: '#f5f5f5ff'
};

// Export type definition for TypeScript type checking
export type ConfigType = typeof config;