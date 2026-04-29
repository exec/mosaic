import {defineConfig} from 'vitest/config';
import solidPlugin from 'vite-plugin-solid';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  plugins: [solidPlugin(), tailwindcss()],
  test: {
    environment: 'happy-dom',
    setupFiles: ['./test/setup.ts'],
  },
});
