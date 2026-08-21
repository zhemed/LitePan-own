import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

// 构建产物写入 Go 内嵌目录，开发态将 /api 代理到后端。
export default defineConfig({
  plugins: [
    vue({
      template: {
        compilerOptions: {
          isCustomElement: (tag) => tag.startsWith("media-"),
        },
      },
    }),
  ],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: process.env.LITEPAN_API_PROXY || "http://127.0.0.1:5211",
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: "../internal/api/web",
    emptyOutDir: true,
    // 解码器按需分包，阈值略高于当前最大独立产物。
    chunkSizeWarningLimit: 3200,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules/pdfjs-dist')) return 'pdf-vendor';
          if (id.includes('node_modules/heic-to')) return 'heic-vendor';
          if (id.includes('node_modules/hls.js') || id.includes('node_modules/mpegts.js')) return 'media-vendor';
          if (id.includes('node_modules/xlsx')) return 'xlsx-vendor';
          if (id.includes('node_modules/libbitsub')) return 'subtitle-vendor';
          if (id.includes('node_modules/docx-preview') || id.includes('node_modules/rtf.js')) return 'doc-vendor';
          if (id.includes('node_modules/@aiden0z/pptx-renderer')) return 'pptx-vendor';
          if (id.includes('node_modules/zip.js')) return 'zip-vendor';
          if (id.includes('node_modules/three')) return 'three-vendor';
          if (id.includes('node_modules/vue') || id.includes('node_modules/pinia') || id.includes('node_modules/vue-router')) return 'vue-vendor';
        }
      }
    }
  },
});
