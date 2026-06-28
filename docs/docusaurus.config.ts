import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const config: Config = {
  title: 'zeep-orbit',
  tagline: 'One backend for all your AI-generated frontends',
  favicon: 'img/favicon.ico',

  url: 'https://zeeplabs.github.io',
  baseUrl: '/zeep-orbit/',
  organizationName: 'zeeplabs',
  projectName: 'zeep-orbit',
  trailingSlash: false,

  onBrokenLinks: 'throw',
  onBrokenMarkdownLinks: 'warn',

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          sidebarPath: './sidebars.ts',
          editUrl: 'https://github.com/zeeplabs/zeep-orbit/edit/main/docs/',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    image: 'img/og-image.png',
    colorMode: {
      defaultMode: 'dark',
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'zeep-orbit',
      logo: {
        alt: 'zeep-orbit',
        src: 'img/logo.svg',
      },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docsSidebar',
          position: 'left',
          label: 'Docs',
        },
        {
          href: 'https://github.com/zeeplabs/zeep-orbit',
          label: 'GitHub',
          position: 'right',
        },
        {
          href: 'https://github.com/zeeplabs/zeep-orbit/discussions',
          label: 'Discussions',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Docs',
          items: [
            {label: 'Quick Start', to: '/docs/quickstart'},
            {label: 'Configuration', to: '/docs/configuration'},
            {label: 'API Reference', to: '/docs/api/crud'},
          ],
        },
        {
          title: 'Community',
          items: [
            {label: 'GitHub Discussions', href: 'https://github.com/zeeplabs/zeep-orbit/discussions'},
            {label: 'Issues', href: 'https://github.com/zeeplabs/zeep-orbit/issues'},
          ],
        },
        {
          title: 'More',
          items: [
          {label: 'GitHub', href: 'https://github.com/zeeplabs/zeep-orbit'},
            {label: 'Zeep Tecnologia', href: 'https://zeeptech.com.br'},
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Zeep Tecnologia. Built with Docusaurus.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
