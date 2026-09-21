import eslintJs from '@eslint/js';
import globals from 'globals';
import tseslint from 'typescript-eslint';
import { defineConfig, globalIgnores } from 'eslint/config';
import stylistic from '@stylistic/eslint-plugin';
import betterTailwindcss from 'eslint-plugin-better-tailwindcss';
import eslintReact from '@eslint-react/eslint-plugin';
import storybook from 'eslint-plugin-storybook';
import { dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));

// Carried over from OpenIPC/fancyweb-ng, where it existed but was never run:
// there was no `lint` script and no lint step in CI. It runs here, and
// `--max-warnings=0` is what makes that mean something.
export default defineConfig([
  globalIgnores(['dist/**', 'storybook-static/**', 'src/vendor/**']),
  {
    languageOptions: { globals: { ...globals.browser, ...globals.node } },
  },
  {
    files: ['**/*.ts', '**/*.tsx'],
    extends: [
      eslintJs.configs.recommended,
      ...tseslint.configs.recommended,
      eslintReact.configs['recommended-typescript'],
      betterTailwindcss.configs.recommended,
      betterTailwindcss.configs.stylistic,
      ...storybook.configs['flat/recommended'],
    ],
    plugins: { '@stylistic': stylistic },
    languageOptions: {
      parser: tseslint.parser,
      parserOptions: { projectService: true, tsconfigRootDir: here },
    },
    rules: {
      // `_` and `_e` mean "required by the signature, not used here".
      '@typescript-eslint/no-unused-vars': ['error', {
        argsIgnorePattern: '^_',
        varsIgnorePattern: '^_',
        caughtErrorsIgnorePattern: '^_',
      }],
    },
    settings: {
      'better-tailwindcss': { cwd: here, entryPoint: 'src/styles/index.css' },
    },
  },
]);
