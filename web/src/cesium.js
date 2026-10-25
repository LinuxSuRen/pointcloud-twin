// Cesium 场景封装：查看器初始化（不依赖 ion）、3D Tiles 加载、
// 设备代理体实体管理与点选交互。
import * as Cesium from 'cesium'
import { createPanel } from './panel.js'

// createViewer 构造一个无影像底图、无 ion 依赖的查看器（离线可用）。
export function createViewer(container) {
  const viewer = new Cesium.Viewer(container, {
    baseLayer: false,
    baseLayerPicker: false,
    geocoder: false,
    homeButton: false,
    sceneModePicker: false,
    navigationHelpButton: false,
    animation: false,
    timeline: false,
    fullscreenButton: false,
    infoBox: false,
    selectionIndicator: false,
    shouldAnimate: false,
  })
  viewer.scene.globe.baseColor = Cesium.Color.fromCssColorString('#1c2733')
  viewer.scene.backgroundColor = Cesium.Color.fromCssColorString('#10151c')
  viewer.scene.skyAtmosphere.show = false
  return viewer
}

// loadTileset 加载点云底图并定位视角。
export async function loadTileset(viewer, url) {
  const tileset = await Cesium.Cesium3DTileset.fromUrl(url, {
    skipLevelOfDetail: false,
  })
  tileset.style = new Cesium.Cesium3DTileStyle({ pointSize: 3 })
  viewer.scene.primitives.add(tileset)
  await viewer.zoomTo(tileset)
  return tileset
}

// enuFrame 返回某 ENU 原点的正逆变换（局部 ENU ↔ ECEF）。
export function enuFrame(origin) {
  const o = origin ?? { lon: 0, lat: 0, h: 0 }
  const originCartesian = Cesium.Cartesian3.fromDegrees(o.lon, o.lat, o.h || 0)
  const matrix = Cesium.Transforms.eastNorthUpToFixedFrame(originCartesian)
  const inverse = Cesium.Matrix4.inverse(matrix, new Cesium.Matrix4())
  return { matrix, inverse }
}

// addDeviceEntities 依据代理体配置生成设备实体：盒面贴动态数据面板
// （Canvas 纹理），描边随状态变色。返回 deviceId → {entity, panel} 映射。
export function addDeviceEntities(viewer, devices, origin) {
  const frame = enuFrame(origin)
  const byId = new Map()
  for (const d of devices) {
    byId.set(d.deviceId, addOneDevice(viewer, d, frame))
  }
  return byId
}

// addOneDevice 生成单台设备的实体（新建与编辑后重建共用）。
export function addOneDevice(viewer, device, frame) {
  const center = Cesium.Matrix4.multiplyByPoint(
    frame.matrix,
    new Cesium.Cartesian3(device.center[0], device.center[1], device.center[2]),
    new Cesium.Cartesian3(),
  )
  const hpr = new Cesium.HeadingPitchRoll(Cesium.Math.toRadians(device.heading || 0), 0, 0)
  const orientation = Cesium.Transforms.headingPitchRollQuaternion(center, hpr)
  const panel = createPanel(device)
  const entity = viewer.entities.add({
    id: device.deviceId,
    position: center,
    orientation,
    box: {
      dimensions: new Cesium.Cartesian3(device.size[0], device.size[1], device.size[2]),
      material: new Cesium.ImageMaterialProperty({ image: panel.canvas, transparent: false }),
      outline: true,
      outlineColor: Cesium.Color.fromCssColorString('#3d9ff0'),
    },
    label: {
      text: device.deviceId,
      font: '12px sans-serif',
      fillColor: Cesium.Color.fromCssColorString('#c8d2de'),
      showBackground: true,
      backgroundColor: Cesium.Color.fromCssColorString('#171e27').withAlpha(0.85),
      pixelOffset: new Cesium.Cartesian2(0, -14),
      distanceDisplayCondition: new Cesium.DistanceDisplayCondition(0, 400),
    },
  })
  return { entity, panel }
}

// setupPicking 注册点选：命中代理体时回调 deviceId。
export function setupPicking(viewer, onPick) {
  const handler = new Cesium.ScreenSpaceEventHandler(viewer.scene.canvas)
  handler.setInputAction((click) => {
    const picks = viewer.scene.drillPick(click.position)
    for (const p of picks) {
      if (p?.id && typeof p.id === 'string' && onPick) {
        onPick(p.id, click.position)
        return
      }
    }
    onPick(null, click.position)
  }, Cesium.ScreenSpaceEventType.LEFT_CLICK)
  return handler
}

// pickLocalPoint 在窗口坐标处拾取点云/地形表面点，返回局部 ENU 坐标（米）。
// 拾取失败（点到天空等）返回 null。
export function pickLocalPoint(viewer, windowPos, frame) {
  if (!viewer.scene.pickPositionSupported) {
    return null
  }
  const cartesian = viewer.scene.pickPosition(windowPos)
  if (!cartesian) {
    return null
  }
  const local = Cesium.Matrix4.multiplyByPoint(frame.inverse, cartesian, new Cesium.Cartesian3())
  return [local.x, local.y, local.z]
}

// addMarkerPoint 画一个临时标记点（标注角点提示用）。
export function addMarkerPoint(viewer, cartesian, colorCss = '#ffbd2e') {
  return viewer.entities.add({
    position: cartesian,
    point: {
      pixelSize: 10,
      color: Cesium.Color.fromCssColorString(colorCss),
      outlineColor: Cesium.Color.fromCssColorString('#10151c'),
      outlineWidth: 2,
    },
  })
}

// localToCartesian 把局部 ENU 坐标转回世界坐标。
export function localToCartesian(frame, x, y, z) {
  return Cesium.Matrix4.multiplyByPoint(frame.matrix, new Cesium.Cartesian3(x, y, z), new Cesium.Cartesian3())
}

// flyToDevice 相机飞向设备代理体。
export function flyToDevice(viewer, entity) {
  if (!entity?.position) {
    return
  }
  viewer.flyTo(entity, { duration: 0.8 })
}

// setEntityStatus 依据状态颜色更新代理体描边（盒面已由数据面板纹理承载）。
export function setEntityStatus(entity, cssColor) {
  if (!entity?.box) {
    return
  }
  entity.box.outlineColor = Cesium.Color.fromCssColorString(cssColor)
}
