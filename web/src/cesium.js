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

// addDeviceEntities 依据代理体配置生成设备实体：盒面贴动态数据面板
// （Canvas 纹理），描边随状态变色。返回 deviceId → {entity, panel} 映射。
export function addDeviceEntities(viewer, devices, origin) {
  const originCarto = origin ?? { lon: 0, lat: 0, h: 0 }
  const originCartesian = Cesium.Cartesian3.fromDegrees(originCarto.lon, originCarto.lat, originCarto.h || 0)
  const enu = Cesium.Transforms.eastNorthUpToFixedFrame(originCartesian)
  const byId = new Map()

  for (const d of devices) {
    const center = Cesium.Matrix4.multiplyByPoint(
      enu,
      new Cesium.Cartesian3(d.center[0], d.center[1], d.center[2]),
      new Cesium.Cartesian3(),
    )
    const hpr = new Cesium.HeadingPitchRoll(Cesium.Math.toRadians(d.heading || 0), 0, 0)
    const orientation = Cesium.Transforms.headingPitchRollQuaternion(center, hpr)
    const panel = createPanel(d)
    const entity = viewer.entities.add({
      id: d.deviceId,
      position: center,
      orientation,
      box: {
        dimensions: new Cesium.Cartesian3(d.size[0], d.size[1], d.size[2]),
        material: new Cesium.ImageMaterialProperty({ image: panel.canvas, transparent: false }),
        outline: true,
        outlineColor: Cesium.Color.fromCssColorString('#3d9ff0'),
      },
      label: {
        text: d.deviceId,
        font: '12px sans-serif',
        fillColor: Cesium.Color.fromCssColorString('#c8d2de'),
        showBackground: true,
        backgroundColor: Cesium.Color.fromCssColorString('#171e27').withAlpha(0.85),
        pixelOffset: new Cesium.Cartesian2(0, -14),
        distanceDisplayCondition: new Cesium.DistanceDisplayCondition(0, 400),
      },
    })
    byId.set(d.deviceId, { entity, panel })
  }
  return byId
}

// setupPicking 注册点选：命中代理体时回调 deviceId。
export function setupPicking(viewer, onPick) {
  const handler = new Cesium.ScreenSpaceEventHandler(viewer.scene.canvas)
  handler.setInputAction((click) => {
    const picks = viewer.scene.drillPick(click.position)
    for (const p of picks) {
      if (p?.id && typeof p.id === 'string' && onPick) {
        onPick(p.id)
        return
      }
    }
    onPick(null)
  }, Cesium.ScreenSpaceEventType.LEFT_CLICK)
  return handler
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
