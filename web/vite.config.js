import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { viteStaticCopy } from 'vite-plugin-static-copy'

// Cesium 的静态资源（Workers/Assets/Widgets）拷贝到产物目录，
// 由 index.html 中的 window.CESIUM_BASE_URL 指向。
export default defineConfig({
  plugins: [
    vue(),
    viteStaticCopy({
      targets: [
        { src: 'node_modules/cesium/Build/Cesium/Workers', dest: 'cesium-static' },
        { src: 'node_modules/cesium/Build/Cesium/ThirdParty', dest: 'cesium-static' },
        { src: 'node_modules/cesium/Build/Cesium/Assets', dest: 'cesium-static' },
        { src: 'node_modules/cesium/Build/Cesium/Widgets', dest: 'cesium-static' },
      ],
    }),
  ],
  server: {
    proxy: {
      // 开发态把瓦片与 WebSocket 代理到 mock 服务器（pnpm mock）
      '/tiles': 'http://127.0.0.1:8787',
      '/ws': { target: 'ws://127.0.0.1:8787', ws: true },
      // 转换服务（pc2twin serve）——网页导入 PCD 的上传/任务/SSE 接口
      '/api': 'http://127.0.0.1:8080',
    },
  },
  test: {
    environment: 'node',
    include: ['src/**/*.spec.js'],
  },
})
