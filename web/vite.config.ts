import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// 开发时把 API 与图片路径代理到本地 Go 进程:cookie 同源,无需 CORS。
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      "/api": "http://127.0.0.1:8080",
      "/i": "http://127.0.0.1:8080",
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
