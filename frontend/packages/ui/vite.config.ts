import { defineConfig } from 'vitest/config';
import preact from '@preact/preset-vite';
import svgr from 'vite-plugin-svgr';
import tailwindcss from '@tailwindcss/vite';
import { resolve } from 'node:path';

// One config, no modes. fancyweb-ng's vite.config.ts switched on `mode` and
// threw "Unknown config modifiers" for anything it did not name -- which is
// why `storybook build` has never worked there: Storybook builds with
// mode='production'. A library config has nothing to switch on.
export default defineConfig({
  plugins: [
    preact(),
    svgr({ include: '**/*.svg?react' }),
    tailwindcss(),
  ],
  build: {
    lib: {
      entry: resolve(import.meta.dirname, 'src/index.ts'),
      formats: ['es'],
      fileName: () => 'openipc-ui.js',
    },
    rollupOptions: {
      // The host app brings its own Preact; two copies would mean two hook
      // dispatchers and components that render but never update.
      external: [/^preact($|\/)/],
    },
    sourcemap: true,
    target: 'es2022',
  },
  test: {
    environment: 'jsdom',
    globals: true,
    include: ['src/**/*.test.{ts,tsx}'],
    setupFiles: ['./vitest.setup.ts'],
  },
});
