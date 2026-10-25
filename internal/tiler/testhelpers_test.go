package tiler

import "github.com/LinuxSuRen/pointcloud-twin/internal/geo"

func ENUToECEFSimple(lon, lat, h float64) [16]float64 {
	return geo.ENUToECEFMatrix(lon, lat, h)
}
