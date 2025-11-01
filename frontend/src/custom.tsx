// 网页自定义配置文件
export const config = {
  // 网页标题
  pageTitle: 'Example Networks - Looking Glass',
  
  // 右侧页脚文字内容
  footerRightText: '© 2025 Example Networks, LLC.',
  
  // 网页icon图标路径，请把图像文件放到public/images目录下
  faviconPath: '/images/favicon.png',
  
  // 网页左上角logo图标路径，请把图像文件放到public/images目录下
  logoPath: '/images/logo.png',
  
  // 网页背景颜色
  backgroundColor: '#f5f4f1'
};

// 导出类型定义，方便TypeScript类型检查
export type ConfigType = typeof config;