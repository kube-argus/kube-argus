// @ts-check
const { themes } = require('prism-react-renderer');

/** @type {import('@docusaurus/types').Config} */
const config = {
  title: 'Kargus',
  tagline: "Drive Kubernetes RBAC from your identity provider's group membership",
  favicon: 'img/kargus-mark.svg',

  url: 'https://kargus.io',
  baseUrl: '/',

  organizationName: 'kube-argus',
  projectName: 'kargus',
  onBrokenLinks: 'throw',

  i18n: { defaultLocale: 'en', locales: ['en'] },

  markdown: { mermaid: true, hooks: { onBrokenMarkdownLinks: 'warn' } },
  themes: ['@docusaurus/theme-mermaid'],

  presets: [
    [
      'classic',
      /** @type {import('@docusaurus/preset-classic').Options} */
      ({
        docs: {
          routeBasePath: '/docs',
          sidebarPath: require.resolve('./sidebars.js'),
          editUrl:
            'https://github.com/kube-argus/kube-argus/tree/main/docs/',
        },
        blog: false,
        theme: { customCss: require.resolve('./src/css/custom.css') },
      }),
    ],
  ],

  themeConfig:
    /** @type {import('@docusaurus/preset-classic').ThemeConfig} */
    ({
      colorMode: {
        defaultMode: 'dark',
        respectPrefersColorScheme: false,
      },
      navbar: {
        title: 'Kargus',
        logo: { alt: 'Kargus', src: 'img/kargus-mark.svg' },
        items: [
          { type: 'docSidebar', sidebarId: 'docs', position: 'left', label: 'Docs' },
          {
            href: 'https://github.com/kube-argus/kube-argus',
            label: 'Source',
            position: 'right',
          },
        ],
      },
      footer: {
        style: 'dark',
        copyright: `Copyright © ${new Date().getFullYear()} Kargus.`,
      },
      prism: {
        theme: themes.github,
        darkTheme: themes.dracula,
        additionalLanguages: ['yaml', 'go', 'bash'],
      },
    }),
};

module.exports = config;
