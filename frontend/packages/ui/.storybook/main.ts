import type { StorybookConfig } from '@storybook/preact-vite';

const config: StorybookConfig = {
  stories: ['../src/**/*.stories.@(ts|tsx)'],
  addons: [],
  framework: '@storybook/preact-vite',
  // The library build's `lib` block would otherwise bundle Storybook's own
  // preview into one file named openipc-ui.js.
  viteFinal: async (config) => ({ ...config, build: { ...config.build, lib: undefined } }),
};

export default config;
