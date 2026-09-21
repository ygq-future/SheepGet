import js from './frontend/node_modules/@eslint/js/src/index.js';
export default [
  {
    ...js.configs.recommended,
    files: ['scripts/**/*.mjs', '*.mjs'],
    languageOptions: {
      globals: {
        process: 'readonly',
        console: 'readonly',
        Buffer: 'readonly',
        URL: 'readonly',
        fetch: 'readonly',
      },
    },
  },
];
