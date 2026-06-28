// @ts-check
const { themes } = require('prism-react-renderer');

/** @type {import('@docusaurus/types').Config} */
const config = {
  title: 'Kargos',
  tagline: "Drive Kubernetes RBAC from your identity provider's group membership",
  favicon: 'img/favicon.ico',

  url: 'https://lucasgolino.github.io',
  baseUrl: '/kargos/',

  organizationName: 'lucasgolino',
  projectName: 'kargos',
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
          routeBasePath: '/', // docs-only mode
          sidebarPath: require.resolve('./sidebars.js'),
          editUrl:
            'https://git./kargos/tree/main/docs/',
        },
        blog: false,
        theme: { customCss: require.resolve('./src/css/custom.css') },
      }),
    ],
  ],

  themeConfig:
    /** @type {import('@docusaurus/preset-classic').ThemeConfig} */
    ({
      navbar: {
        title: 'Kargos',
        items: [
          { type: 'docSidebar', sidebarId: 'docs', position: 'left', label: 'Docs' },
          {
            href: 'https://git./kargos',
            label: 'Source',
            position: 'right',
          },
        ],
      },
      footer: {
        style: 'dark',
        copyright: `Copyright © ${new Date().getFullYear()} kargos.`,
      },
      prism: {
        theme: themes.github,
        darkTheme: themes.dracula,
        additionalLanguages: ['yaml', 'go', 'bash'],
      },
    }),
};

module.exports = config;
