import { createApp } from 'vue'
// Cesium widget 样式必须在挂载前生效，否则查看器画布退回默认 300×150 尺寸
import 'cesium/Build/Cesium/Widgets/widgets.css'
import App from './App.vue'
import './style.css'

createApp(App).mount('#app')
