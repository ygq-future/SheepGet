import base from '../.prettierrc.json' with { type: 'json' };
import * as tailwind from 'prettier-plugin-tailwindcss';
export default {
  ...base,
  plugins: [tailwind],
  tailwindStylesheet: './src/style.css',
  tailwindFunctions: ['cn'],
};
