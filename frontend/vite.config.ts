import {defineConfig} from 'vitest/config';
import solidPlugin from 'vite-plugin-solid';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  plugins: [solidPlugin(), tailwindcss()],
  build: {
    rollupOptions: {
      output: {
        // Keep uPlot in its own chunk so the lazily-loaded Speed tab
        // pulls the charting library on demand rather than bundling it
        // into the initial app payload.
        manualChunks: {
          uplot: ['uplot'],
        },
      },
    },
  },
  test: {
    environment: 'happy-dom',
    setupFiles: ['./test/setup.ts'],
  },
});
